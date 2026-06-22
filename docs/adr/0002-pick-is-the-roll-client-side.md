# The Pick is the roll: selection is client-side and emergent from the animation

When the user triggers a Pick, the random selection is produced *by the
animation itself* on the client — like physically rolling a die — and is not
predetermined by the server. Only after the animation lands on a task does the
client send a request to move that task to In Progress. The server validates
the request (task still Pending, under the in-progress limit) and returns an
error toast if it cannot honour it.

We deliberately rejected the more testable alternative — server picks a random
task up front and the animation merely plays toward a known result. That makes
the animation theater over a decided outcome, which removes the meaning the
ritual is meant to carry. Here the outcome is genuinely unknown until it lands.

**Consequence:** the *distribution* of the pick (e.g. uniformity across the
pool) is not unit-testable, because it emerges from animation timing rather than
a pure function. Everything after the landing — the transition, server-side
validation, the disabled-at-limit guard, error handling — remains testable.
Fairness of the draw is a property of the animation's design, to be considered
when that animation is built.
