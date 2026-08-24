package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/sanverite/pgpool/internal/config"
	"github.com/sanverite/pgpool/internal/protocol"
)

func main() {
	// Structured logger. Every log line is JSON in production.
	// We write to stdout - systemd captures it via journald.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Replace the default logger so any package that calls
	// slog.Info() directly uses our configured logger.
	slog.SetDefault(logger)

	// flag.String registers a command line flag.
	// Arguments: flag name, default value, description.
	// Returns a pointer to the value - we dereference it after parse.
	configPath := flag.String("config", "", "path to config file (required)")

	// flag.Parse reads os.Args and populates all registered flags.
	// Must be called before accessing any flags.
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintf(os.Stderr, "error: --config is required\n")
		os.Exit(1)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	slog.Info("pgpool starting",
		"listen", cfg.Server.Addr(),
		"backend", cfg.Backend.Host,
		"pool_size", cfg.Pool.Size,
		"mode", cfg.Pool.Mode,
	)

	// signal.NotifyContext gives us a context that is cancelled
	// the moment the OS sends SIGINT (CTRL+C) or SIGTERM (systemd stop).
	// This single context is passed down through everything -
	// when it cancels, the entire program knows to stop.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	// stop() deregisters the signal handler and releases resources. Always call it, hence defer.
	defer stop()

	if err := run(ctx, cfg); err != nil {
		// We don't use log.Fatal or fmt.Println for errors.
		// fmt.Fprintf to stderr + os.Exit(1) is the correct pattern
		// for a binary's fatal error path.
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	slog.Info("pgpool stopped")
}

// run is the real entry point. main() does signal handling and
// error reporting. run() does everything else.
//
// Separating them means run() is testable - you can call it
// directly in tests with a cancellable context without having
// to deal with os.Exit or signals.
func run(ctx context.Context, cfg config.Config) error {
	// net.Listen creates a TCP listener on the given address.
	// "tcp" means IPv4 and IPv6. The listner is not yet
	// accepting connections - it's just bound to the port.
	ln, err := net.Listen("tcp", cfg.Server.Addr())
	if err != nil {
		return fmt.Errorf("listening on %s: %w", cfg.Server.Addr(), err)
	}

	slog.Info("listening", "addr", cfg.Server.Addr())

	// We launch Accept in a separate goroutine so the main
	// goroutine can watch for context cancellation (CTRL+C)
	// and close the listener cleanly.
	//
	// errCh carries the error from the accept loop back to
	// the main goroutine. Buffered with 1 so the goroutine
	// never blocks on send even if we've already returned.
	errCh := make(chan error, 1)

	go func() {
		errCh <- serve(ln)
	}()

	// Block until either:
	// - ctx is cancelled (CTRL+C or SIGTERM) -> graceful shutdown
	// - serve() returns an error -> something went wrong
	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")

		// Closing the listener cause Accept() to return
		// immediately with an error, which exits the serve loop.
		if err := ln.Close(); err != nil {
			slog.Warn("closing listener", "err", err)
		}

		// Wait for serve() to finish after we closed the listener.
		// We expect an error here (use of closed network connection)
		// so we discard it.
		<-errCh
		return nil

	case err := <-errCh:
		// serve() exited on its own - this is unexpected
		return fmt.Errorf("server exited: %w", err)
	}
}

// serve runs the accept loop. It blocks until the listener is closed.
// Each accepted connection is handled in its goroutine.
func serve(ln net.Listener) error {
	for {
		// Accepts blocks until a client connects or the listener
		// is closed. On close it returns an error - that's our
		// signal to exit the loop.
		conn, err := ln.Accept()
		if err != nil {
			// When we close the listener intentionally during
			// shutdown, Accept returns an error containing
			// "use of closed network connection". We treat
			// any Accept error as a signal to stop serving.
			return fmt.Errorf("accepting connection: %w", err)
		}

		slog.Info("client connected",
			"remote_addr", conn.RemoteAddr().String(),
		)

		// Each connection get its own goroutine.
		// handleConn is responsible for closing conn when done.
		go handleConn(conn)
	}
}

// handleConn handles a single client connection.
// For now it reads whatever the client sends and logs it.
// Next step: parse the Postgres wire protocol.
func handleConn(conn net.Conn) {
	// Always close the connection when we're done.
	// defer guarantees this even if we panic or return early.
	defer conn.Close()

	defer slog.Info("client disconnected",
		"remote_addr", conn.RemoteAddr().String(),
	)

	// Parse the startup message - the first thing every
	// Postgres client sends when it connects.
	msg, err := protocol.ReadStartupMessage(conn)
	if err != nil {
		slog.Error("reading startup message",
			"remote_addr", conn.RemoteAddr().String(),
			"err", err,
		)
		return
	}

	// Log what client told us about itself.
	slog.Info("client startup",
		"remote_addr", conn.RemoteAddr().String(),
		"user", msg.Parameters["user"],
		"database", msg.Parameters["database"],
		"application_name", msg.Parameters["application_name"],
		"protocol_version", msg.ProtocolVersion,
	)

	// Send AuthenticationOK
	if err := protocol.WriteAuthenticationOK(conn); err != nil {
		slog.Error("writing auth ok",
			"remote_addr", conn.RemoteAddr().String(),
			"err", err,
		)
		return
	}

	// Send a minimal set of ParameterStatus messages.
	params := [][2]string{
		{"server_version", "17.0"},
		{"client_encoding", "UTF8"},
		{"DateStyle", "ISO, MDY"},
		{"TimeZone", "UTC"},
	}

	for _, p := range params {
		if err := protocol.WriteParameterStatus(conn, p[0], p[1]); err != nil {
			slog.Error("writing parameter status",
				"remote_addr", conn.RemoteAddr().String(),
				"err", err,
			)
			return
		}
	}

	// Send fake BackendKeyData.
	if err := protocol.WriteBackendKeyData(conn, 0, 0); err != nil {
		slog.Error("writing backend key data",
			"remote_addr", conn.RemoteAddr().String(),
			"err", err,
		)
		return
	}

	// Send ReadyForQuery with status 'I' - idle, ready for commands.
	if err := protocol.WriteReadyForQuery(conn, protocol.ReadyStatusIdle); err != nil {
		slog.Error("writing ready for query",
			"remote_addr", conn.RemoteAddr().String(),
			"err", err,
		)
		return
	}

	slog.Info("client ready",
		"remote_addr", conn.RemoteAddr().String(),
		"user", msg.Parameters["user"],
	)

	// Read and handle messages from the client.
	for {
		msg, err := protocol.ReadMessage(conn)
		if err != nil {
			// io.EOF means the client closed the connection cleanly.
			// Any other error is unexpected.
			if errors.Is(err, io.EOF) {
				return
			}
			slog.Error("reading client message",
				"remote_addr", conn.RemoteAddr().String(),
				"err", err,
			)
			return
		}

		switch msg.Type {

		case protocol.MessageTypeQuery:
			// Client sent a query.
			query, err := msg.QueryString()
			if err != nil {
				slog.Error("parsing query",
					"remote_addr", conn.RemoteAddr().String(),
					"err", err,
				)
				return
			}

			slog.Info("received query",
				"remote_addr", conn.RemoteAddr().String(),
				"query", query,
			)

			// Send ReadyForQuery so psql shows the prompt again.
			if err := protocol.WriteReadyForQuery(conn, protocol.ReadyStatusIdle); err != nil {
				slog.Error("writing ready for query",
					"remote_addr", conn.RemoteAddr().String(),
					"err", err,
				)
				return
			}

		case protocol.MessageTypeTerminate:
			// Client sent 'X' - clean disconnect.
			slog.Info("client terminated cleanly",
				"remote_addr", conn.RemoteAddr().String(),
			)
			return

		default:
			// Unkown message type - log and continue.
			slog.Warn("unkown message type",
				"remote_addr", conn.RemoteAddr().String(),
				"type", string(msg.Type),
			)
		}
	}
}
