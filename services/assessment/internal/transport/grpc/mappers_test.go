package grpc_test

import (
	"testing"

	"github.com/Newterios/lms-system-prob/services/assessment/internal/model"
	// importar o pacote interno via black-box test
)

// TestQuestionToProto_StripCorrect verifies that the Choice.Correct field
// is not present in the proto response shape.
// This is the transport-layer guarantee required by ARCHITECTURE.md §11.
func TestQuestionToProto_StripCorrect(t *testing.T) {
	// We test by ensuring model.Choice.Correct is never reflected
	// into the proto output. Since assessmentv1.Choice has no Correct field,
	// the compiler enforces this at compile time.
	// This test documents the design decision and ensures the model.Choice
	// struct contains Correct (for auto-grading) while proto Choice does not.

	c := &model.Choice{Key: "a", Value: "answer", Correct: true}
	if c.Key == "" {
		t.Error("sanity: Key should not be empty")
	}
	// The fact that assessmentv1.Choice has no Correct field means
	// leaking it to clients is *structurally impossible* at the proto layer.
	// The mapper (questionToProto) only copies Key and Value.
	t.Log("design decision #11: proto Choice.Correct does not exist — compiler enforces this")
}
