# DESIGN.md — pgpool

## Status
ACCEPTED

## Author
Sankalp

## Last updated
2026-08-20

---

## 1. Problem

Every Postgres connection is a forked OS process. It consumes memory
whether it is doing work or not. Applications that maintain large
connection pools — or many app instances each with their own pool —
exhaust Postgres long before they exhaust application resources.

pgpool sits between applications and Postgres, multiplexing many client
connections onto a small fixed set of real backend connections. Apps
think they have a dedicated connection. They don't.

---

## 2. Goals

- Accept TCP connections from Postgres clients
- Speak the Postgres wire protocol — not a dumb byte pipe
- Maintain a fixed pool of real backend connections to Postgres
- Implement three pooling modes: session, transaction, statement
- Queue clients when the pool is exhausted, reject after timeout
- Health check backend connections, remove and replace dead ones
- Run as a systemd service on bare metal (anton)
- Expose Prometheus metrics for observability via homelab LGTM stack
- Survive crashes and restart automatically via systemd

---

## 3. Non-goals

- No support for multiple Postgres servers (no HA or failover)
- No TLS termination in v1
- No support for databases other than Postgres
- No query routing or read/write splitting
- No admin console in v1

---

## 4. Pooling modes

### Session mode (first to build)
A client holds a backend connection for its entire session.
Released only on client disconnect.

### Transaction mode (core mode)
A client holds a backend connection only for the duration of a
transaction. Released when ReadyForQuery with status 'I' is received.
This is the mode that provides real multiplexing.

### Statement mode (last to build)
A client holds a backend connection for a single statement.
Released after each CommandComplete + ReadyForQuery sequence.
Breaks anything that uses transactions — use with care.

---

## 5. What happens when pool is exhausted

When all backend connections are busy and a new client connects:

1. Client is placed in a FIFO queue
2. Queue has a configurable max depth (default: 100)
3. If a backend becomes free, the first queued client gets it
4. If the client waits longer than queue_timeout (default: 30s),
   it receives an ErrorResponse and the connection is closed
5. If the queue is full, the client is rejected immediately

---

## 6. Architecture

### 6.1 Package layout

```
pgpool/
├── cmd/
│ └── pgpool/
│ └── main.go # entry point
├── internal/
│ ├── config/ # config loading, validation
│ │ └── config.go
│ ├── pool/ # connection pool core
│ │ ├── pool.go # pool interface + implementation
│ │ └── pool_test.go
│ ├── proxy/ # client <-> backend proxying
│ │ ├── proxy.go # handles one client connection
│ │ └── proxy_test.go
│ ├── protocol/ # postgres wire protocol parsing
│ │ ├── message.go # message types, reader, writer
│ │ ├── startup.go # startup message parsing
│ │ ├── auth.go # auth message handling
│ │ └── protocol_test.go
│ ├── metrics/ # prometheus metrics
│ │ └── metrics.go
│ └── health/ # backend health checking
│ └── health.go
├── deploy/
│ └── pgpool.service # systemd unit file
├── Makefile
├── DESIGN.md
├── STANDARDS.md
├── CHANGELOG.md
└── docs/
└── adr/
├── ADR-001-protocol-parsing-depth.md
├── ADR-002-pooling-mode-default.md
└── ADR-003-queue-vs-reject.md
```

### 6.2 Core interfaces

```go
// Pool manages backend connections.
type Pool interface {
    Acquire(ctx context.Context) (BackendConn, error)
    Release(conn BackendConn)
    Close() error
    Stats() PoolStats
}

// BackendConn is a single real connection to Postgres.
type BackendConn interface {
    Read(b []byte) (n int, err error)
    Write(b []byte) (n int, err error)
    Close() error
    IsAlive() bool
}

// Proxy handles a single client connection end to end.
type Proxy interface {
    Handle(ctx context.Context, client net.Conn) error
}
```

### 6.3 Request lifecycle

1. TCP connection accepted from client
2. Read StartupMessage — extract user, database, app_name
3. Acquire backend connection from pool
4. if pool exhausted → queue client
5. if queue timeout → send ErrorResponse, close
6. Perform auth handshake with backend
7. Replay ParameterStatus messages to client
8. Send fake BackendKeyData to client (intercept cancellation)
   Send ReadyForQuery to client
   Enter proxy loop:
   read message from client → forward to backend
   read response from backend → forward to client
   watch for ReadyForQuery with status 'I'
   if transaction mode → release backend to pool
9. On client disconnect → release backend to pool
10. On backend failure → remove from pool, replace with new connection

### 6.4 Deployment on anton

```
anton (bare metal)
├── systemd
│ └── pgpool.service # Restart=always, starts on boot
├── /etc/pgpool/
│ └── pgpool.toml # config file
└── /usr/local/bin/
└── pgpool # the binary
```

Apps on k3s connect to anton's Tailscale IP on port 5433.
pgpool forwards to Postgres on port 5432 on the same machine
or anywhere reachable on the Tailscale network.

---

## 7. Configuration

```toml
[server]
host = "0.0.0.0"
port = 5433

[backend]
host = "localhost"
port = 5432
database = "mydb"
user = "pgpool"
password = "secret"

[pool]
size = 20
mode = "transaction"
queue_timeout = "30s"
queue_max_depth = 100
health_check_interval = "10s"

[metrics]
enabled = true
port = 9090
```

---

## 8. Metrics exposed

```
pgpool_client_connections_total # total clients connected
pgpool_pool_size # configured pool size
pgpool_pool_active # connections currently in use
pgpool_pool_idle # connections currently idle
pgpool_queue_depth # clients currently waiting
pgpool_queue_timeout_total # clients rejected due to timeout
pgpool_backend_errors_total # backend connection failures
pgpool_query_duration_seconds # histogram of query durations
```

---

## 9. Systemd unit file

```ini
[Unit]
Description=pgpool - Postgres connection pooler
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/pgpool --config /etc/pgpool/pgpool.toml
Restart=always
RestartSec=5s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

---

## 10. Build and deploy

```
make build # build binary
make test # run tests
make race # run tests with race detector
make deploy # scp binary to anton, restart systemd service
```

---

## 11. What we explicitly defer

- TLS between client and pgpool
- TLS between pgpool and Postgres
- Multiple backend servers
- Admin interface
- Statement timeout enforcement
