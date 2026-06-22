# Tasky

A Postgres-backed web app that holds a pool of tasks and, on demand, picks one
at random for the user to do — surfaced through a button that plays a selection
animation.

## Language

**Task**:
A unit of work the user can be asked to do (e.g. "wash the dishes"). The central
entity. What a task represents is up to the user — household chores, errands,
study items. Not recurring: each task moves through its status lifecycle once
and does not return to the pool on its own.
_Avoid_: Chore, todo, item

**Pick**:
The act of selecting one task at random and moving it from Pending to
In Progress. Triggered by the user and accompanied by an animation. A Pick does
not complete the task — the user marks it Completed afterwards.
_Avoid_: Draw, roll, spin, select

**Status**:
Where a task is in its lifecycle. One of:
- **Pending** — in the pool, eligible to be picked. _Avoid_: todo, open, new
- **In Progress** — picked, currently being done. _Avoid_: active, doing
- **Completed** — finished. Terminal; the task does not recur. _Avoid_: done

**Pool**:
The set of Pending tasks — the only tasks eligible to be picked. Excludes
Cancelled tasks.

**Cancel**:
The user decides not to do a task and removes it from every view. A soft
operation: the record is retained, just filtered out everywhere. Can apply to a
task in any status.
_Avoid_: Delete, remove, archive
