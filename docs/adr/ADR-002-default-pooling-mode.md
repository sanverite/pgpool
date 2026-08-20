# ADR-002 — Default pooling mode

## Status
Accepted

## Date
2026-08-20

## Context

pgpool supports three pooling modes: session, transaction, statement.
We need a sensible default for users who don't explicitly configure one.

Session mode is the safest — never breaks application behaviour —
but provides no real multiplexing. Defeats the primary purpose of
a connection pooler.

Transaction mode provides real multiplexing but risks leaking
connection state between clients. Specifically:

- SET variables persist on the connection
- Prepared statements persist
- Advisory locks held across transactions persist
- Temporary tables persist
- LISTEN registrations persist

## Decision

Default to transaction mode. Run the following reset query every time
a backend connection is returned to the pool:

    DISCARD ALL;

This resets all connection state to a clean baseline before the
connection is handed to the next client.

Known exceptions where session mode is required:
- Apps using advisory locks across transaction boundaries
- Apps using temporary tables across transaction boundaries  
- Apps using LISTEN/NOTIFY with persistent subscriptions

These cases are documented. Users with these requirements set
mode = "session" explicitly in config.

## Consequences

- Most applications work correctly out of the box
- Every connection release incurs one extra round trip (DISCARD ALL)
- High throughput systems can override reset_query with something
  lighter or empty if they manage state themselves
- Apps with the documented exceptions will silently misbehave
  if they use transaction mode — this is called out clearly in
  the README
