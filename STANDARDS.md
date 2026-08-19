# STANDARDS.md — pgpool

## Code

- No error is ignored. Ever. No `_` on an error return.
- Every error is wrapped with context:
  `fmt.Errorf("acquiring backend connection: %w", err)`
- No panics in library code. Panics only in main, only for
  unrecoverable startup failures.
- No global state outside of main. Everything is passed explicitly.
- Interfaces are defined by the consumer, not the producer.
- No package imports cycles. Ever.

## Naming

- Follow standard Go naming. No hungarian notation, no type suffixes.
- Acronyms are all caps: `pgpool`, `TCPConn`, `URL`, not `TcpConn`.
- Unexported by default. Export only what another package needs.

## Error messages

- Lowercase, no punctuation at the end.
- Wrap with context so the full chain reads naturally:
  "starting server: listening on :5433: bind: address already in use"

## Testing

- Every non-trivial function has a test.
- Table-driven tests with `t.Run` for all cases.
- No real network calls in tests. Use `net.Pipe()` or fakes.
- `make race` passes clean before every commit.
- Test file lives next to the file it tests: `pool_test.go`

## Commits

- Present tense, imperative: "add health check" not "added health check"
- No "fix bug", "update things", "WIP". Every commit message
  explains what and why in one line.
- One logical change per commit.

## Formatting and lint

- `gofmt` always. No exceptions.
- `golangci-lint run` passes clean before every PR.
- No commented out code committed.

## Concurrency

- Every goroutine has a clear owner and a clear exit condition.
- Every goroutine is waited on before the program exits.
- Context cancellation is propagated everywhere.
- `make race` is the final check before every merge.

## Logging

- Structured logging via `slog` only.
- No `fmt.Println` outside of main.
- Log levels mean something:
    DEBUG — useful during development, off in production
    INFO  — normal operational events
    WARN  — something unexpected but recoverable
    ERROR — something failed, needs attention
- Every log line has context: which connection, which client, which backend.

## Graceful shutdown

- On SIGINT or SIGTERM:
    1. Stop accepting new connections
    2. Wait for in-flight proxying to complete
    3. Release all backend connections cleanly
    4. Exit with code 0
- Shutdown has a timeout. If it takes longer than 30s, force exit.

## Makefile targets

Every project has these, always:

    make build      # build the binary
    make test       # run all tests
    make race       # run tests with race detector
    make lint       # run golangci-lint
    make deploy     # ship to anton
    make clean      # remove build artifacts
