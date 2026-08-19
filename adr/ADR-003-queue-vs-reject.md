# ADR-003 — Behaviour when pool is exhausted

## Status
Accepted

## Date
2026-08-20

## Context

When all backend connections are busy, clients must wait. We need
to define exactly what happens at each stage of exhaustion.

Three states to handle:

1. Pool exhausted, queue has space
2. Pool exhausted, queue is full
3. Client has been queued longer than queue_timeout

## Decision

**State 1 — Pool exhausted, queue has space**
Client is placed at the back of a FIFO queue.
When a backend connection is released, the front of the queue
gets it immediately.

**State 2 — Pool exhausted, queue is full**
Client is rejected immediately with a Postgres ErrorResponse:

    SQLSTATE 53300 — too_many_connections
    Message: "pgpool: connection queue full, try again later"

No waiting. The system is already under significant load.
Adding more waiters increases pressure without benefit.

**State 3 — Client has waited longer than queue_timeout**
Client is removed from the queue and rejected with:

    SQLSTATE 57014 — query_canceled  
    Message: "pgpool: queue timeout exceeded"

## Configuration

```toml
[pool]
queue_timeout   = "30s"   # how long a client waits before rejection
queue_max_depth = 100     # max clients in queue before immediate rejection
```

## Consequences

- Behaviour under load is predictable and fast
- Clients get a real Postgres error they can handle
- No thundering herd — queue_max_depth caps total waiting clients
- Operators can tune both values based on their workload
- Monitoring queue_depth metric gives early warning before
  queue_max_depth is hit
