package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/Newterios/lms-system-prob/services/assessment/internal/event"
	"github.com/Newterios/lms-system-prob/services/assessment/internal/model"
	"github.com/Newterios/lms-system-prob/services/assessment/internal/usecase/port"
	"github.com/google/uuid"
)

// GradeSubmissionUseCase applies a teacher's manual score inside a DB transaction
// and writes an outbox entry in the same TX (ARCHITECTURE.md §4.3).
type GradeSubmissionUseCase struct {
	attempts port.AttemptRepository
	quizzes  port.QuizRepository
	outbox   port.OutboxRepository
	cache    port.Cache
	tx       port.TxManager
}

func NewGradeSubmissionUseCase(attempts port.AttemptRepository, quizzes port.QuizRepository, outbox port.OutboxRepository, cache port.Cache, tx port.TxManager) *GradeSubmissionUseCase {
	return &GradeSubmissionUseCase{attempts: attempts, quizzes: quizzes, outbox: outbox, cache: cache, tx: tx}
}

type GradeSubmissionInput struct {
	AttemptID   uuid.UUID
	CallerID    uuid.UUID // must be teacher of the quiz
	ManualScore float64
}

type GradeSubmissionOutput struct{ Attempt *model.Attempt }

func (uc *GradeSubmissionUseCase) Execute(ctx context.Context, in GradeSubmissionInput) (GradeSubmissionOutput, error) {
	attempt, err := uc.attempts.GetByID(ctx, in.AttemptID)
	if err != nil {
		return GradeSubmissionOutput{}, err
	}
	if attempt.Status != "submitted" {
		return GradeSubmissionOutput{}, fmt.Errorf("attempt not submitted: %w", model.ErrFailedPrecondition)
	}

	// verify teacher owns the quiz
	quiz, err := uc.quizzes.GetByID(ctx, attempt.QuizID)
	if err != nil {
		return GradeSubmissionOutput{}, err
	}
	if quiz.TeacherID != in.CallerID {
		return GradeSubmissionOutput{}, fmt.Errorf("not quiz owner: %w", model.ErrPermissionDenied)
	}

	attempt.ManualScore = &in.ManualScore
	attempt.Status = "graded"

	payload := event.Marshal("assessment.submission.graded", attempt.ID.String(), map[string]any{
		"manual_score": in.ManualScore,
		"course_id":    quiz.CourseID.String(),
	})

	// ACID: update attempt + insert outbox in one TX
	if err := uc.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := uc.attempts.Update(ctx, attempt); err != nil {
			return err
		}
		return uc.outbox.Insert(ctx, &model.OutboxEntry{
			AggregateID: attempt.ID,
			EventType:   "assessment.submission.graded",
			Payload:     payload,
			OccurredAt:  time.Now().UTC(),
		})
	}); err != nil {
		return GradeSubmissionOutput{}, err
	}

	// invalidate gradebook cache for the course
	_ = uc.cache.Delete(ctx, fmt.Sprintf("assessment:gradebook:%s", quiz.CourseID))
	return GradeSubmissionOutput{Attempt: attempt}, nil
}

