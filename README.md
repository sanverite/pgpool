# pgpool

A production-grade PostgreSQL connection pooler written from scratch in Go.
Sits between your applications and Postgres, multiplexing many client
connections onto a small fixed set of real backend connections.

Built as a learning project to understand Go, TCP networking, and the
Postgres wire protocol at depth. Not a wrapper around PgBouncer —
every byte of the protocol is implemented by hand.

## How it works

Without pgpool, every app connection maps to one real Postgres connection.
Each real connection is a forked OS process consuming memory whether it is
doing work or not. Postgres degrades significantly past a few hundred
connections.

pgpool sits in the middle:

```
App 1 ─┐
App 2 ─┤
App 3 ─┼──▶ pgpool ──▶ 20 real Postgres connections
App 4 ─┤
App 5 ─┘
```


In transaction mode, a client holds a backend connection only for the
duration of a transaction. Between transactions the connection returns
to the pool and is available for other clients.

## Pooling modes

| Mode        | Connection held until    | Use when                          |
|-------------|--------------------------|-----------------------------------|
| session     | client disconnects       | app uses advisory locks or LISTEN |
| transaction | transaction ends         | most applications (default)       |
| statement   | statement completes      | maximum throughput, no transactions|

## Configuration

Create a JSON config file:

```json
{
    "server": {
        "host": "0.0.0.0",
        "port": 5433
    },
    "backend": {
        "host": "localhost",
        "port": 5432,
        "database": "mydb",
        "user": "myuser",
        "password": "secret"
    },
    "pool": {
        "size": 20,
        "mode": "transaction",
        "queue_timeout": "30s",
        "queue_max_depth": 100,
        "reset_query": "DISCARD ALL"
    },
    "metrics": {
        "enabled": true,
        "port": 9090
    }
}
```

All fields are optional — missing fields use the defaults above.

## Usage

```bash
# Build
make build

# Run
./bin/pgpool --config /path/to/config.json

# Connect via psql (same as connecting to Postgres directly)
psql -h localhost -p 5433 -U myuser -d mydb
```

## Development

```bash
make build    # build the binary to bin/pgpool
make test     # run all tests
make race     # run tests with the race detector
make lint     # run golangci-lint
make clean    # remove build artifacts
```

## Deployment

pgpool is designed to run as a systemd service on bare metal.

```bash
make deploy                        # copy binary to /usr/local/bin/pgpool on anton
sudo systemctl enable pgpool
sudo systemctl start pgpool
sudo journalctl -fu pgpool         # follow logs
```

## Roadmap

- [x] TCP listener
- [x] Postgres wire protocol parsing
- [x] SSL negotiation
- [x] Auth handshake
- [x] Query message handling
- [ ] Dumb proxy — forward to real Postgres
- [ ] Connection pool
- [ ] Transaction mode
- [ ] Prometheus metrics
- [ ] systemd deployment on anton

## Architecture

```
cmd/pgpool/ — binary entry point, signal handling, startup
internal/config/ — config loading and validation
internal/protocol/ — postgres wire protocol parsing and writing
internal/proxy/ — client connection handling
internal/pool/ — connection pool
internal/health/ — backend health checking
internal/metrics/ — prometheus metrics
deploy/ — systemd unit file
docs/adr/ — architecture decision records
```
