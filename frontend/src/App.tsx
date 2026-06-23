import { useEffect, useState, type FormEvent } from "react";
import { api, type Task } from "./api/client";

// Disposable prototype UI: create a task, see the active pool, and Pick a
// Pending task into In Progress. Intentionally minimal — the Pick animation and
// polish come in later slices (PRD: don't gold-plate). The Pick button here is
// plain (un-animated) and picks any Pending task to exercise the server path.
export function App() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [maxInProgress, setMaxInProgress] = useState(1);
  const [title, setTitle] = useState("");
  const [notes, setNotes] = useState("");
  const [error, setError] = useState<string | null>(null);

  async function refresh() {
    const { data, error } = await api.GET("/tasks");
    if (error) {
      setError("Could not load tasks");
      return;
    }
    setTasks(data.active ?? []);
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

  // Pick the first Pending task. The server independently validates the pick;
  // a rejection (limit reached, not Pending) surfaces as a toast, not a crash.
  async function onPick() {
    setError(null);
    const pending = tasks.find((t) => t.status === "pending");
    if (!pending) return;
    const { error } = await api.POST("/tasks/{id}/pick", {
      params: { path: { id: pending.id } },
    });
    if (error) {
      setError("Could not pick a task — try again.");
      await refresh();
      return;
    }
    await refresh();
  }

  const inProgressCount = tasks.filter((t) => t.status === "in_progress").length;
  const pendingCount = tasks.filter((t) => t.status === "pending").length;
  const pickDisabled = pendingCount === 0 || inProgressCount >= maxInProgress;

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
        Pick
      </button>

      {error && <p style={{ color: "crimson" }}>{error}</p>}

      <h2>Active</h2>
      {tasks.length === 0 ? (
        <p>No tasks yet.</p>
      ) : (
        <ul>
          {tasks.map((task) => {
            const inProgress = task.status === "in_progress";
            return (
              <li
                key={task.id}
                style={
                  inProgress
                    ? { background: "#fff3bf", padding: "2px 4px", borderRadius: 4 }
                    : undefined
                }
              >
                <strong>{task.title}</strong> <em>({task.status})</em>
                {task.notes && <div>{task.notes}</div>}
              </li>
            );
          })}
        </ul>
      )}
    </main>
  );
}
