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

// ErrCompleteRejected is returned when a Complete matches no eligible row: the
// Task is not In Progress (still Pending, already Completed, or cancelled).
// Completed is terminal. The API maps this to a client-visible toast, not a 500.
var ErrCompleteRejected = errors.New("complete rejected: task is not in progress")

// ErrCancelRejected is returned when a Cancel matches no live row: the Task is
// missing or already cancelled (soft-deleted). Cancel is an orthogonal soft
// delete allowed from any status, so the only rejection is "no live row". The
// API maps this to a 404 Not Found, not a 500.
var ErrCancelRejected = errors.New("cancel rejected: task is missing or already cancelled")

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

// ListRecentlyCompleted returns Tasks completed within the rolling 24h window,
// newest-first — shown below the active list.
func (s *Service) ListRecentlyCompleted(ctx context.Context) ([]Task, error) {
	return s.store.ListRecentlyCompleted(ctx)
}

// ListOlderCompleted returns Tasks completed before the rolling 24h window,
// newest-first — the history behind the collapsed expand toggle.
func (s *Service) ListOlderCompleted(ctx context.Context) ([]Task, error) {
	return s.store.ListOlderCompleted(ctx)
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

// Complete transitions an In-Progress Task to Completed and stamps the
// completion time, validated server-side in one statement. If the Task is not
// In Progress it returns ErrCompleteRejected (mapped by the API to a toast).
func (s *Service) Complete(ctx context.Context, id int64) (Task, error) {
	return s.store.Complete(ctx, id)
}

// Cancel soft-deletes a Task by stamping deleted_at, allowed from any status.
// It is a single guarded statement: only a live (not already cancelled) row
// matches. If no live row matches (missing or already cancelled) it returns
// ErrCancelRejected (mapped by the API to a 404).
func (s *Service) Cancel(ctx context.Context, id int64) (Task, error) {
	return s.store.Cancel(ctx, id)
}
