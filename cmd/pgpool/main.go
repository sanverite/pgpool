package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
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

	// signal.NotifyContext gives us a context that is cancelled
	// the moment the OS sends SIGINT (CTRL+C) or SIGTERM (systemd stop).
	// This single context is passed down through everything -
	// when it cancels, the entire program knows to stop.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	// stop() deregisters the signal handler and releases resources. Always call it, hence defer.
	defer stop()

	slog.Info("pgpool starting")

	if err := run(ctx); err != nil {
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
func run(ctx context.Context) error {
	// Placeholder - we fill this in next.
	// For now, blocked until context is cancelled (CTRL+C or SIGTERM).
	slog.Info("press CTRL+C to stop")
	<-ctx.Done()
	return nil
}
