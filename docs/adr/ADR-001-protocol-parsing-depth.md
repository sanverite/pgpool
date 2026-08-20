# ADR-001 — Protocol parsing depth

## Status
Accepted

## Date
2026-08-20

## Context

The Postgres wire protocol has dozens of message types. Our pooler
needs to parse the protocol to manage connection lifecycle. The
question is how deep we go.

Two options:

A — Parse only what we need to manage lifecycle:
    - StartupMessage (client → us)
    - AuthenticationX messages (backend → us → client)
    - ParameterStatus (backend → us → client)
    - BackendKeyData (backend → us, we fake it to client)
    - ReadyForQuery (backend → us, signals transaction boundary)
    - ErrorResponse (backend → us → client)
    Everything else is forwarded as raw bytes untouched.

B — Parse every message type fully.
    Gives richer metrics but significantly more code and maintenance
    surface.

## Decision

Option A. We parse the minimum required to correctly manage connection
lifecycle and nothing more. The protocol is complex — parsing
everything in v1 is scope creep that delays a working system.
Deeper parsing can be added later when there is a concrete reason.

## Consequences

- Metrics will be connection-level, not query-level, in v1
- We cannot track individual query durations without deeper parsing
- The protocol layer stays small and auditable
- Adding query-level metrics later is a contained change to
  internal/protocol only
