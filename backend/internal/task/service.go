package task

import (
	"context"
	"errors"
	"strings"
)

// ErrTitleRequired is returned when a Task is created with a blank title. The
// API maps it to a validation response rather than a 500 (cf. ADR-0004's
// guidance on translating store errors into client-visible validation errors).
var ErrTitleRequired = errors.New("title is required")

// ErrPickRejected is returned when a Pick matches no eligible row: the Task is
// no longer Pending (already picked, completed, or cancelled) or the In-Progress
// limit is already reached. The server validates every Pick independently of UI
// state; the API maps this to a client-visible toast, not a 500 (ADR-0002).
var ErrPickRejected = errors.New("pick rejected: task is not pending or the in-progress limit is reached")

// Service holds the Task lifecycle rules. It sits between the HTTP handlers and
// the Store. maxInProgress is the configurable concurrency limit (default 1,
// scales to 3) enforced server-side; it is unused until the Pick slice but is
// held here so the limit lives with the rules that will use it.
type Service struct {
	store         *Store
	maxInProgress int
}

// NewService returns a Service over the given store and in-progress limit.
func NewService(store *Store, maxInProgress int) *Service {
	return &Service{store: store, maxInProgress: maxInProgress}
}

// Create adds a new Pending Task from a title (required) and optional notes.
// The title is trimmed; a title that is blank or whitespace-only is rejected so
// huma's minLength guard can't be bypassed with spaces.
func (s *Service) Create(ctx context.Context, title, notes string) (Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Task{}, ErrTitleRequired
	}
	return s.store.Create(ctx, title, notes)
}

// ListActive returns the active pool (Pending + In Progress), manually ordered.
func (s *Service) ListActive(ctx context.Context) ([]Task, error) {
	return s.store.ListActive(ctx)
}

// MaxInProgress is the configurable concurrency limit, exposed so the API can
// report it to the frontend (which disables the Pick control at the limit).
func (s *Service) MaxInProgress() int {
	return s.maxInProgress
}

// Pick transitions a Pending Task to In Progress, server-validated in one
// transaction: the Task must still be Pending and live, and the current
// In-Progress count must be below the limit. If no eligible row matches, it
// returns ErrPickRejected (mapped by the API to a client toast, not a 500).
func (s *Service) Pick(ctx context.Context, id int64) (Task, error) {
	t, err := s.store.Pick(ctx, id, s.maxInProgress)
	if errors.Is(err, ErrPickRejected) {
		return Task{}, ErrPickRejected
	}
	return t, err
}
