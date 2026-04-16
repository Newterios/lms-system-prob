package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/Newterios/lms-system-prob/services/assessment/internal/model"
	"github.com/Newterios/lms-system-prob/services/assessment/internal/usecase/port"
	"github.com/google/uuid"
)

type StartAttemptUseCase struct {
	quizzes  port.QuizRepository
	attempts port.AttemptRepository
	checker  port.EnrollmentChecker
	events   port.EventPublisher
}

func NewStartAttemptUseCase(quizzes port.QuizRepository, attempts port.AttemptRepository, checker port.EnrollmentChecker, events port.EventPublisher) *StartAttemptUseCase {
	return &StartAttemptUseCase{quizzes: quizzes, attempts: attempts, checker: checker, events: events}
}

type StartAttemptInput struct {
	QuizID    uuid.UUID
	StudentID uuid.UUID // from JWT
}

type StartAttemptOutput struct{ Attempt *model.Attempt }

func (uc *StartAttemptUseCase) Execute(ctx context.Context, in StartAttemptInput) (StartAttemptOutput, error) {
	q, err := uc.quizzes.GetByID(ctx, in.QuizID)
	if err != nil {
		return StartAttemptOutput{}, err
	}

	// enrollment check via cross-service gRPC client
	enrolled, err := uc.checker.IsEnrolled(ctx, q.CourseID, in.StudentID)
	if err != nil {
		// propagate ErrRemoteUnavailable unchanged
		return StartAttemptOutput{}, fmt.Errorf("enrollment check: %w", err)
	}
	if !enrolled {
		return StartAttemptOutput{}, fmt.Errorf("student not enrolled in course: %w", model.ErrFailedPrecondition)
	}

	attempt := &model.Attempt{
		ID:        uuid.Must(uuid.NewRandom()),
		QuizID:    in.QuizID,
		StudentID: in.StudentID,
		StartedAt: time.Now().UTC(),
		Status:    "in_progress",
		Answers:   make(map[uuid.UUID][]string),
	}

	if err := uc.attempts.Create(ctx, attempt); err != nil {
		return StartAttemptOutput{}, err
	}

	// best-effort event (lossy-acceptable per spec — no outbox for StartAttempt)
	payload, _ := json.Marshal(map[string]string{
		"attempt_id": attempt.ID.String(),
		"quiz_id":    attempt.QuizID.String(),
		"student_id": attempt.StudentID.String(),
	})
	if err := uc.events.Publish(ctx, "assessment.attempt.started", payload); err != nil {
		slog.WarnContext(ctx, "publish attempt.started failed", "err", err)
	}

	return StartAttemptOutput{Attempt: attempt}, nil
}

