package task

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// taskDTO is the wire shape of a Task. It is the OpenAPI-described contract the
// generated frontend clients bind to, kept separate from the domain Task so the
// JSON shape and the internal type can evolve independently.
type taskDTO struct {
	ID          int64      `json:"id" doc:"Unique task identifier"`
	Title       string     `json:"title" doc:"Short task title"`
	Notes       string     `json:"notes" doc:"Optional longer detail"`
	Status      string     `json:"status" enum:"pending,in_progress,completed" doc:"Lifecycle status"`
	Position    int        `json:"position" doc:"Manual ordering position within the active pool"`
	CreatedAt   time.Time  `json:"created_at" doc:"When the task was created"`
	CompletedAt *time.Time `json:"completed_at,omitempty" doc:"When the task was completed, if it has been"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty" doc:"When the task was cancelled, if it has been"`
}

func toDTO(t Task) taskDTO {
	return taskDTO{
		ID:          t.ID,
		Title:       t.Title,
		Notes:       t.Notes,
		Status:      string(t.Status),
		Position:    t.Position,
		CreatedAt:   t.CreatedAt,
		CompletedAt: t.CompletedAt,
		DeletedAt:   t.DeletedAt,
	}
}

// toDTOs maps a slice of domain Tasks to wire DTOs, always returning a non-nil
// (possibly empty) slice so each list serialises as [] rather than null.
func toDTOs(tasks []Task) []taskDTO {
	out := make([]taskDTO, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, toDTO(t))
	}
	return out
}

// CreateTaskInput is the POST /tasks request body.
type CreateTaskInput struct {
	Body struct {
		Title string `json:"title" minLength:"1" maxLength:"200" doc:"Short task title"`
		Notes string `json:"notes,omitempty" maxLength:"2000" doc:"Optional longer detail"`
	}
}

// TaskOutput wraps a single task in a response body.
type TaskOutput struct {
	Body taskDTO
}

// ListTasksOutput is the GET /tasks response: the active pool, the completed
// Tasks split by the rolling 24h window (recently vs older, both newest-first),
// and the server-enforced In-Progress limit, so the frontend can disable Pick
// at the limit without a second round-trip.
type ListTasksOutput struct {
	Body struct {
		Active            []taskDTO `json:"active" doc:"Active pool: Pending and In Progress tasks, manually ordered"`
		RecentlyCompleted []taskDTO `json:"recently_completed" doc:"Tasks completed within the last 24h (rolling), newest-first"`
		OlderCompleted    []taskDTO `json:"older_completed" doc:"Tasks completed more than 24h ago, newest-first"`
		MaxInProgress     int       `json:"max_in_progress" doc:"Server-enforced limit on concurrent In Progress tasks"`
	}
}

// PickTaskInput is the POST /tasks/{id}/pick path-only request.
type PickTaskInput struct {
	ID int64 `path:"id" doc:"Identifier of the Pending task to pick"`
}

// CompleteTaskInput is the POST /tasks/{id}/complete path-only request.
type CompleteTaskInput struct {
	ID int64 `path:"id" doc:"Identifier of the In-Progress task to complete"`
}

// CancelTaskInput is the DELETE /tasks/{id} path-only request.
type CancelTaskInput struct {
	ID int64 `path:"id" doc:"Identifier of the task to cancel (soft delete)"`
}

// EditTaskInput is the PATCH /tasks/{id} request: a partial update of title
// and/or notes. Both fields are optional pointers — an omitted field means
// "leave unchanged". Lifecycle fields are not part of the edit contract.
type EditTaskInput struct {
	ID   int64 `path:"id" doc:"Identifier of the task to edit"`
	Body struct {
		Title *string `json:"title,omitempty" minLength:"1" maxLength:"200" doc:"New title (omit to leave unchanged)"`
		Notes *string `json:"notes,omitempty" maxLength:"2000" doc:"New notes (omit to leave unchanged; empty string clears notes)"`
	}
}

// MoveTaskInput is the POST /tasks/{id}/move request: the desired 0-based index
// of the task within the active ordering (active = Pending + In Progress).
type MoveTaskInput struct {
	ID   int64 `path:"id" doc:"Identifier of the active task to move"`
	Body struct {
		Position int `json:"position" minimum:"0" doc:"Desired 0-based index within the active ordering"`
	}
}

// RegisterRoutes registers the task operations on the huma API. The handlers
// are the source of truth from which the OpenAPI spec is generated (ADR-0003).
func RegisterRoutes(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID:   "createTask",
		Method:        http.MethodPost,
		Path:          "/tasks",
		Summary:       "Create a task",
		Description:   "Create a task from a title and optional notes. A created task starts as Pending.",
		Tags:          []string{"tasks"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *CreateTaskInput) (*TaskOutput, error) {
		t, err := svc.Create(ctx, in.Body.Title, in.Body.Notes)
		if err != nil {
			if errors.Is(err, ErrTitleRequired) {
				return nil, huma.Error422UnprocessableEntity("title is required")
			}
			return nil, huma.Error500InternalServerError("could not create task", err)
		}
		return &TaskOutput{Body: toDTO(t)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listTasks",
		Method:      http.MethodGet,
		Path:        "/tasks",
		Summary:     "List active tasks",
		Description: "Return the active pool: Pending and In Progress tasks, manually ordered.",
		Tags:        []string{"tasks"},
	}, func(ctx context.Context, _ *struct{}) (*ListTasksOutput, error) {
		active, err := svc.ListActive(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("could not list tasks", err)
		}
		recent, err := svc.ListRecentlyCompleted(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("could not list tasks", err)
		}
		older, err := svc.ListOlderCompleted(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("could not list tasks", err)
		}
		out := &ListTasksOutput{}
		out.Body.Active = toDTOs(active)
		out.Body.RecentlyCompleted = toDTOs(recent)
		out.Body.OlderCompleted = toDTOs(older)
		out.Body.MaxInProgress = svc.MaxInProgress()
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "pickTask",
		Method:      http.MethodPost,
		Path:        "/tasks/{id}/pick",
		Summary:     "Pick a task",
		Description: "Transition a Pending task to In Progress. The server validates the pick in one transaction: the task must still be Pending and the In-Progress limit must not be reached. A pick that matches no eligible row is rejected with 409 Conflict.",
		Tags:        []string{"tasks"},
	}, func(ctx context.Context, in *PickTaskInput) (*TaskOutput, error) {
		t, err := svc.Pick(ctx, in.ID)
		if err != nil {
			if errors.Is(err, ErrPickRejected) {
				return nil, huma.Error409Conflict("task is not pending or the in-progress limit is reached")
			}
			return nil, huma.Error500InternalServerError("could not pick task", err)
		}
		return &TaskOutput{Body: toDTO(t)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "completeTask",
		Method:      http.MethodPost,
		Path:        "/tasks/{id}/complete",
		Summary:     "Complete a task",
		Description: "Transition an In Progress task to Completed, stamping the completion time. Completed is terminal. Completing a task that is not In Progress is rejected with 409 Conflict.",
		Tags:        []string{"tasks"},
	}, func(ctx context.Context, in *CompleteTaskInput) (*TaskOutput, error) {
		t, err := svc.Complete(ctx, in.ID)
		if err != nil {
			if errors.Is(err, ErrCompleteRejected) {
				return nil, huma.Error409Conflict("task is not in progress")
			}
			return nil, huma.Error500InternalServerError("could not complete task", err)
		}
		return &TaskOutput{Body: toDTO(t)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cancelTask",
		Method:      http.MethodDelete,
		Path:        "/tasks/{id}",
		Summary:     "Cancel a task",
		Description: "Cancel a task as an orthogonal soft delete (stamping deleted_at), allowed from any status. A cancelled task is filtered from every view. Cancelling a task that is missing or already cancelled is rejected with 404 Not Found.",
		Tags:        []string{"tasks"},
	}, func(ctx context.Context, in *CancelTaskInput) (*TaskOutput, error) {
		t, err := svc.Cancel(ctx, in.ID)
		if err != nil {
			if errors.Is(err, ErrCancelRejected) {
				return nil, huma.Error404NotFound("task is missing or already cancelled")
			}
			return nil, huma.Error500InternalServerError("could not cancel task", err)
		}
		return &TaskOutput{Body: toDTO(t)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "editTask",
		Method:      http.MethodPatch,
		Path:        "/tasks/{id}",
		Summary:     "Edit a task",
		Description: "Update a task's title and/or notes (partial update), allowed in any status — Pending, In Progress, or Completed. Lifecycle fields (status, completion, cancellation) are not part of this contract. Editing a missing or cancelled task is rejected with 404 Not Found.",
		Tags:        []string{"tasks"},
	}, func(ctx context.Context, in *EditTaskInput) (*TaskOutput, error) {
		t, err := svc.Edit(ctx, in.ID, in.Body.Title, in.Body.Notes)
		if err != nil {
			if errors.Is(err, ErrTitleRequired) {
				return nil, huma.Error422UnprocessableEntity("title is required")
			}
			if errors.Is(err, ErrTaskNotFound) {
				return nil, huma.Error404NotFound("task not found")
			}
			return nil, huma.Error500InternalServerError("could not edit task", err)
		}
		return &TaskOutput{Body: toDTO(t)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "moveTask",
		Method:      http.MethodPost,
		Path:        "/tasks/{id}/move",
		Summary:     "Move a task",
		Description: "Move a live active task (Pending or In Progress) to a 0-based index within the active ordering, renumbering the whole active set to contiguous positions in one transaction. The position is clamped to the bounds of the active list, so an out-of-range index lands the task last. Moving a task that is missing, completed, or cancelled is rejected with 404 Not Found.",
		Tags:        []string{"tasks"},
	}, func(ctx context.Context, in *MoveTaskInput) (*TaskOutput, error) {
		t, err := svc.Move(ctx, in.ID, in.Body.Position)
		if err != nil {
			if errors.Is(err, ErrMoveRejected) {
				return nil, huma.Error404NotFound("task is not a live active task")
			}
			return nil, huma.Error500InternalServerError("could not move task", err)
		}
		return &TaskOutput{Body: toDTO(t)}, nil
	})
}
