package port

import (
	"context"
	"testing"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

type fakeAgent struct {
	id     model.AgentID
	result AgentResult
}

func (f fakeAgent) ID() model.AgentID { return f.id }
func (f fakeAgent) Run(context.Context, model.AgentInput) (AgentResult, error) {
	return f.result, nil
}

var _ Agent = fakeAgent{}

func TestAgent_ImplementableWithConcreteResult(t *testing.T) {
	t.Parallel()
	got := fakeAgent{
		id:     model.AgentTypeClassifier,
		result: &model.ClassificationResult{ContractType: model.ContractTypeServices, Confidence: 1.0},
	}
	var a Agent = got
	if a.ID() != model.AgentTypeClassifier {
		t.Fatalf("ID mismatch: got=%v", a.ID())
	}
	res, err := a.Run(context.Background(), model.AgentInput{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	cr, ok := res.(*model.ClassificationResult)
	if !ok || cr == nil || cr.ContractType != model.ContractTypeServices {
		t.Fatalf("Run result not narrowable to *model.ClassificationResult: %T", res)
	}
}
