import { useEffect, useState, type FormEvent } from "react";
import { api, type Task } from "./api/client";

// Disposable prototype UI: create a task and see the active pool. Intentionally
// minimal — the animation and polish come in later slices (PRD: don't gold-plate).
export function App() {
  const [tasks, setTasks] = useState<Task[]>([]);
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

      {error && <p style={{ color: "crimson" }}>{error}</p>}

      <h2>Active</h2>
      {tasks.length === 0 ? (
        <p>No tasks yet.</p>
      ) : (
        <ul>
          {tasks.map((task) => (
            <li key={task.id}>
              <strong>{task.title}</strong> <em>({task.status})</em>
              {task.notes && <div>{task.notes}</div>}
            </li>
          ))}
        </ul>
      )}
    </main>
  );
}
