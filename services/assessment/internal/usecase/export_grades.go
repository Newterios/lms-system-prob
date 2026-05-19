package usecase

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"time"

	"github.com/Newterios/lms-system-prob/services/assessment/internal/usecase/port"
	"github.com/google/uuid"
)

type ExportGradesUseCase struct {
	attempts port.AttemptRepository
	quizzes  port.QuizRepository
	cache    port.Cache
}

func NewExportGradesUseCase(attempts port.AttemptRepository, quizzes port.QuizRepository, cache port.Cache) *ExportGradesUseCase {
	return &ExportGradesUseCase{attempts: attempts, quizzes: quizzes, cache: cache}
}

type ExportGradesInput struct{ CourseID uuid.UUID }

type ExportGradesOutput struct {
	CSV      []byte
	Filename string
}

func (uc *ExportGradesUseCase) Execute(ctx context.Context, in ExportGradesInput) (ExportGradesOutput, error) {
	gbUC := NewGetGradebookUseCase(uc.attempts, uc.quizzes, uc.cache)
	gb, err := gbUC.Execute(ctx, GetGradebookInput{CourseID: in.CourseID})
	if err != nil {
		return ExportGradesOutput{}, err
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"student_id", "quiz_id", "quiz_title", "score", "status"})
	for _, e := range gb.Entries {
		_ = w.Write([]string{
			e.StudentID.String(),
			e.QuizID.String(),
			e.QuizTitle,
			fmt.Sprintf("%.2f", e.Score),
			e.Status,
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return ExportGradesOutput{}, fmt.Errorf("csv flush: %w", err)
	}

	filename := fmt.Sprintf("gradebook_%s_%s.csv", in.CourseID, time.Now().UTC().Format("20060102"))
	return ExportGradesOutput{CSV: buf.Bytes(), Filename: filename}, nil
}

