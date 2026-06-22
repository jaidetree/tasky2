// Package task is the single concern module for Tasky: it owns the Task domain
// type and its lifecycle, the Postgres store, the service rules, and the HTTP
// handlers. Organized by concern, not layered by handler/service/repo.
package task

import "time"

// Status is where a Task sits in its lifecycle. A Task flows pending →
// in_progress → completed exactly once; it does not recur. Cancellation is an
// orthogonal soft delete (deleted_at), not a status.
type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
)

// Task is the central domain entity — a unit of work the user can be asked to
// do. It carries the system timestamps even when unset so later slices (the
// rolling-24h completed split, ordering) have the fields they need.
type Task struct {
	ID          int64
	Title       string
	Notes       string
	Status      Status
	Position    int
	CreatedAt   time.Time
	CompletedAt *time.Time
	DeletedAt   *time.Time
}
