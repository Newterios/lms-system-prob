package usecase

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Newterios/lms-system-prob/services/assessment/internal/usecase/port"
	"github.com/google/uuid"
)

type GradebookEntry struct {
	StudentID uuid.UUID
	QuizID    uuid.UUID
	QuizTitle string
	Score     float64
	Status    string
}

type GetGradebookUseCase struct {
	attempts port.AttemptRepository
	quizzes  port.QuizRepository
	cache    port.Cache
}

func NewGetGradebookUseCase(attempts port.AttemptRepository, quizzes port.QuizRepository, cache port.Cache) *GetGradebookUseCase {
	return &GetGradebookUseCase{attempts: attempts, quizzes: quizzes, cache: cache}
}

type GetGradebookInput struct{ CourseID uuid.UUID }

type GetGradebookOutput struct{ Entries []*GradebookEntry }

const gradebookCacheTTL = 60 * quizCacheTTL // same 60s

func (uc *GetGradebookUseCase) Execute(ctx context.Context, in GetGradebookInput) (GetGradebookOutput, error) {
	key := fmt.Sprintf("assessment:gradebook:%s", in.CourseID)
	if b, err := uc.cache.Get(ctx, key); err == nil && len(b) > 0 {
		var entries []*GradebookEntry
		if json.Unmarshal(b, &entries) == nil {
			return GetGradebookOutput{Entries: entries}, nil
		}
	}

	attempts, err := uc.attempts.ListByCourseID(ctx, in.CourseID)
	if err != nil {
		return GetGradebookOutput{}, err
	}

	// build quiz title map
	quizTitles := map[uuid.UUID]string{}

	var entries []*GradebookEntry
	for _, a := range attempts {
		title, ok := quizTitles[a.QuizID]
		if !ok {
			q, err := uc.quizzes.GetByID(ctx, a.QuizID)
			if err == nil {
				title = q.Title
			}
			quizTitles[a.QuizID] = title
		}

		score := 0.0
		if a.ManualScore != nil {
			score = *a.ManualScore
		} else if a.AutoScore != nil {
			score = *a.AutoScore
		}

		entries = append(entries, &GradebookEntry{
			StudentID: a.StudentID,
			QuizID:    a.QuizID,
			QuizTitle: title,
			Score:     score,
			Status:    a.Status,
		})
	}

	if b, err := json.Marshal(entries); err == nil {
		_ = uc.cache.Set(ctx, key, b, gradebookCacheTTL)
	}
	return GetGradebookOutput{Entries: entries}, nil
}

