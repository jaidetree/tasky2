import { useEffect, useRef, useState, type FormEvent } from "react";
import { Reorder } from "motion/react";
import { api, type Task } from "./api/client";

// Disposable prototype UI: create a task, see the active pool, and Pick a
// Pending task into In Progress. The Pick animation IS the draw (ADR-0002): a
// highlight cycles down the Pool with an outdent for physicality, decelerating
// (ease-out) onto the winner, then auto-commits via the Pick endpoint.

// Ease-out timing for the highlight cycle. Returns the delay (ms) before the
// step AFTER index `i` of `total` — small early (fast cycling), large at the
// end (the highlight visibly slows and settles). This is purely the FEEL of the
// deceleration: it changes WHEN each step happens, never WHICH index is landed
// on (that is fixed up-front as start + steps), so easing cannot bias the draw.
function stepDelay(i: number, total: number): number {
  const progress = total <= 1 ? 1 : i / (total - 1); // 0 → 1 across the cycle
  const eased = 1 - (1 - progress) * (1 - progress); // ease-out (quadratic)
  return 55 + eased * 280; // ~55ms fast → ~335ms slow on the final step
}

export function App() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [recentlyCompleted, setRecentlyCompleted] = useState<Task[]>([]);
  const [olderCompleted, setOlderCompleted] = useState<Task[]>([]);
  const [maxInProgress, setMaxInProgress] = useState(1);
  const [title, setTitle] = useState("");
  const [notes, setNotes] = useState("");
  const [error, setError] = useState<string | null>(null);
  // Older completed history is collapsed by default (PRD: available, not in my face).
  const [showOlder, setShowOlder] = useState(false);
  // Index (within the Pending Pool) currently lit by the cycling highlight, or
  // null when no animation is running.
  const [highlightIndex, setHighlightIndex] = useState<number | null>(null);
  const animatingRef = useRef(false);
  // The active order as the server last gave it, so a drag-drop can diff the
  // settled order against the pre-drag baseline (Reorder mutates `tasks` live).
  const orderRef = useRef<Task[]>([]);
  // Inline edit: the id of the row being edited (null = none), plus the draft
  // title/notes. Edit is allowed in any status, so this drives every list.
  const [editingId, setEditingId] = useState<number | null>(null);
  const [draftTitle, setDraftTitle] = useState("");
  const [draftNotes, setDraftNotes] = useState("");

  async function refresh() {
    const { data, error } = await api.GET("/tasks");
    if (error) {
      setError("Could not load tasks");
      return;
    }
    const active = data.active ?? [];
    setTasks(active);
    orderRef.current = active;
    setRecentlyCompleted(data.recently_completed ?? []);
    setOlderCompleted(data.older_completed ?? []);
    setMaxInProgress(data.max_in_progress);
  }

  useEffect(() => {
    refresh();
  }, []);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    const { error } = await api.POST("/tasks", {
      body: { title, notes: notes || undefined },
    });
    if (error) {
      setError("Could not create task");
      return;
    }
    setTitle("");
    setNotes("");
    await refresh();
  }

  // Commit a Pick to the server. The server independently validates it (still
  // Pending, under the limit); a rejection surfaces as a toast, not a crash.
  async function commitPick(id: number) {
    const { error } = await api.POST("/tasks/{id}/pick", {
      params: { path: { id } },
    });
    if (error) {
      setError("Could not pick a task — try again.");
    }
    await refresh();
  }

  // Pick = the animation. The highlight cycles through the Pending Pool and
  // decelerates onto a winner; the landed Task is what we commit (ADR-0002, the
  // result emerges from the animation rather than being pre-decided).
  //
  // Fairness tuning: with a fixed start index, landing on `(start + steps) % n`
  // is uniform across the Pool only if `steps` is drawn so every residue mod n
  // is equally likely. We draw `steps` uniformly from a window of EXACTLY `n`
  // consecutive values (one full extra lap past a guaranteed `baseLaps`), so
  // each Pending task is ~equally likely to win. The ease-out deceleration is
  // applied to step TIMING only and never to which index wins, so it can't bias
  // the draw.
  async function onPick() {
    if (animatingRef.current) return;
    setError(null);
    const pendingTasks = tasks.filter((t) => t.status === "pending");
    const n = pendingTasks.length;
    if (n === 0) return;

    animatingRef.current = true;
    const start = 0;
    const baseLaps = 2; // ensures a satisfying minimum of cycling before landing
    const steps = baseLaps * n + Math.floor(Math.random() * n); // window of n values
    const winnerIndex = (start + steps) % n;
    const winner = pendingTasks[winnerIndex];

    let index = start;
    setHighlightIndex(index);
    for (let i = 0; i < steps; i++) {
      await new Promise((resolve) => setTimeout(resolve, stepDelay(i, steps)));
      index = (index + 1) % n;
      setHighlightIndex(index);
    }

    setHighlightIndex(null);
    animatingRef.current = false;
    await commitPick(winner.id);
  }

  // Complete an In-Progress task. The server validates the transition (only
  // In Progress → Completed); a rejection surfaces as the existing message.
  async function onComplete(id: number) {
    setError(null);
    const { error } = await api.POST("/tasks/{id}/complete", {
      params: { path: { id } },
    });
    if (error) {
      setError("Could not complete the task — try again.");
    }
    await refresh();
  }

  // Cancel a task (soft delete), allowed from any status. The server filters
  // cancelled tasks from every view; a rejection surfaces as the existing message.
  async function onCancel(id: number) {
    setError(null);
    const { error } = await api.DELETE("/tasks/{id}", {
      params: { path: { id } },
    });
    if (error) {
      setError("Could not cancel the task — try again.");
    }
    await refresh();
  }

  // Persist a manual reorder. Reorder updates `tasks` optimistically as the row
  // is dragged; on drop the dragged row tells us exactly which task moved (its
  // own id), so we send that task to its settled index. Using the dragged id —
  // rather than diffing for the first changed slot — is correct in BOTH
  // directions: a downward drag vacates a slot that the shifted-up neighbour
  // fills, so a first-difference heuristic would mis-identify the move and only
  // nudge it one place. Skip the request when the order is unchanged.
  async function commitReorder(movedId: number, order: Task[]) {
    const before = orderRef.current.map((t) => t.id);
    const after = order.map((t) => t.id);
    if (before.length === after.length && before.every((id, i) => id === after[i])) {
      return; // order unchanged → no request
    }
    const position = after.indexOf(movedId);
    const { error } = await api.POST("/tasks/{id}/move", {
      params: { path: { id: movedId } },
      body: { position },
    });
    if (error) {
      setError("Could not move the task — try again.");
    }
    await refresh();
  }

  // Begin editing a row: stash its current title/notes as the draft.
  function startEdit(task: Task) {
    setError(null);
    setEditingId(task.id);
    setDraftTitle(task.title);
    setDraftNotes(task.notes ?? "");
  }

  function cancelEdit() {
    setEditingId(null);
  }

  // Save the inline edit via PATCH (partial update of title/notes), then refresh.
  // The server allows edits in any status; a rejection surfaces as the message.
  async function saveEdit(id: number) {
    setError(null);
    const { error } = await api.PATCH("/tasks/{id}", {
      params: { path: { id } },
      body: { title: draftTitle, notes: draftNotes },
    });
    if (error) {
      setError("Could not save the edit — try again.");
      return;
    }
    setEditingId(null);
    await refresh();
  }

  // Reusable inline edit form, shown on any row across all three lists.
  function editForm(id: number) {
    return (
      <div style={{ display: "grid", gap: 4, marginTop: 4 }}>
        <input
          value={draftTitle}
          onChange={(e) => setDraftTitle(e.target.value)}
          placeholder="Title"
        />
        <textarea
          value={draftNotes}
          onChange={(e) => setDraftNotes(e.target.value)}
          placeholder="Notes (optional)"
        />
        <div>
          <button onClick={() => saveEdit(id)} disabled={!draftTitle.trim()}>
            Save
          </button>
          <button onClick={cancelEdit} style={{ marginLeft: 8 }}>
            Cancel
          </button>
        </div>
      </div>
    );
  }

  const inProgressCount = tasks.filter((t) => t.status === "in_progress").length;
  const pendingCount = tasks.filter((t) => t.status === "pending").length;
  const animating = highlightIndex !== null;
  const pickDisabled =
    pendingCount === 0 || inProgressCount >= maxInProgress || animating;

  // Maps a Pending task id to its index within the Pool, so a row can tell
  // whether the cycling highlight is currently on it.
  const pendingIndexById = new Map(
    tasks.filter((t) => t.status === "pending").map((t, i) => [t.id, i]),
  );

  return (
    <main style={{ maxWidth: 480, margin: "2rem auto", fontFamily: "sans-serif" }}>
      <h1>Tasky</h1>

      <form onSubmit={onSubmit} style={{ display: "grid", gap: 8, marginBottom: 24 }}>
        <input
          placeholder="Task title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          required
        />
        <textarea
          placeholder="Notes (optional)"
          value={notes}
          onChange={(e) => setNotes(e.target.value)}
        />
        <button type="submit" disabled={!title.trim()}>
          Add task
        </button>
      </form>

      <button onClick={onPick} disabled={pickDisabled} style={{ marginBottom: 16 }}>
        {animating ? "Picking…" : "Pick"}
      </button>

      {error && <p style={{ color: "crimson" }}>{error}</p>}

      <h2>Active</h2>
      {tasks.length === 0 ? (
        <p>No tasks yet.</p>
      ) : (
        <Reorder.Group
          axis="y"
          values={tasks}
          onReorder={setTasks}
          style={{ listStyle: "none", padding: 0 }}
        >
          {tasks.map((task) => {
            const inProgress = task.status === "in_progress";
            const lit = pendingIndexById.get(task.id) === highlightIndex;
            return (
              <Reorder.Item
                key={task.id}
                value={task}
                // Drag-to-reorder updates `tasks` live; on drop we persist the
                // settled order with a single move (only if it actually changed).
                onDragEnd={() => commitReorder(task.id, tasks)}
                // The Pick outdent: the lit row shifts right for physicality. A
                // short spring snaps it in/out so each step has a tactile beat
                // that rides the ease-out deceleration of the cycle.
                animate={{ x: lit ? 16 : 0 }}
                transition={{ type: "spring", stiffness: 700, damping: 30 }}
                style={{
                  padding: "2px 4px",
                  borderRadius: 4,
                  cursor: "grab",
                  background: lit
                    ? "#ffd43b"
                    : inProgress
                      ? "#fff3bf"
                      : "transparent",
                }}
              >
                <strong>{task.title}</strong> <em>({task.status})</em>
                {inProgress && (
                  <button onClick={() => onComplete(task.id)} style={{ marginLeft: 8 }}>
                    Complete
                  </button>
                )}
                <button onClick={() => startEdit(task)} style={{ marginLeft: 8 }}>
                  Edit
                </button>
                <button onClick={() => onCancel(task.id)} style={{ marginLeft: 8 }}>
                  Cancel
                </button>
                {editingId === task.id ? editForm(task.id) : task.notes && <div>{task.notes}</div>}
              </Reorder.Item>
            );
          })}
        </Reorder.Group>
      )}

      {recentlyCompleted.length > 0 && (
        <>
          <h2>Recently completed</h2>
          <ul>
            {recentlyCompleted.map((task) => (
              <li key={task.id}>
                {task.title}
                <button onClick={() => startEdit(task)} style={{ marginLeft: 8 }}>
                  Edit
                </button>
                {editingId === task.id && editForm(task.id)}
              </li>
            ))}
          </ul>
        </>
      )}

      {olderCompleted.length > 0 && (
        <section style={{ marginTop: 16 }}>
          <button onClick={() => setShowOlder((v) => !v)}>
            {showOlder ? "Hide" : "Show"} older completed ({olderCompleted.length})
          </button>
          {showOlder && (
            <ul>
              {olderCompleted.map((task) => (
                <li key={task.id}>
                  {task.title}
                  <button onClick={() => startEdit(task)} style={{ marginLeft: 8 }}>
                    Edit
                  </button>
                  {editingId === task.id && editForm(task.id)}
                </li>
              ))}
            </ul>
          )}
        </section>
      )}
    </main>
  );
}
