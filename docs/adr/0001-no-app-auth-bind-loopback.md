# No application-level auth; loopback-only by default

Tasky ships with no login, sessions, or per-user authorization. It is a
single-tenant personal tool intended to run only on a trusted private network,
so authentication is handled at the network layer rather than in the app. This
is a common posture for self-hosted single-user projects and is acceptable here
because little or no sensitive data is expected.

To make that posture safe by default, the server **binds to `127.0.0.1`** unless
explicitly told otherwise. Exposing it on a network is a deliberate act: the
operator must set the `TASKY_HOST` env var. With no override, the app is
reachable only from the same machine and cannot be accidentally served to a
network.

The override is a free-form address. Operators should bind to a specific private
interface address; `0.0.0.0` is permitted but is not the default and is
discouraged, because it listens on every interface — including a public one if
the host has a public IP.

**Consequence:** the app must never be exposed to the public internet as-is.
Doing so requires building real authentication first. Multi-user support, if
ever needed, is an additive migration (introduce a `users` table, add and
backfill `user_id`), not a precondition we pay for now.
