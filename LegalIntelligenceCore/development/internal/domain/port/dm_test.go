package port

import (
	"context"
	"testing"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

type fakeArtifactRequester struct{}

func (fakeArtifactRequester) RequestArtifacts(
	context.Context, string, string, string, string, string, []model.ArtifactType,
) error {
	return nil
}

type fakeAnalysisArtifactsPublisher struct{}

func (fakeAnalysisArtifactsPublisher) Publish(context.Context, LegalAnalysisArtifactsReady) error {
	return nil
}

type fakeArtifactsAwaiter struct{}

func (fakeArtifactsAwaiter) Register(string) (<-chan ArtifactsProvided, error) {
	ch := make(chan ArtifactsProvided)
	return ch, nil
}
func (fakeArtifactsAwaiter) Await(context.Context, string) (ArtifactsProvided, error) {
	return ArtifactsProvided{}, nil
}
func (fakeArtifactsAwaiter) Cancel(string) {}

type fakePersistConfirmationAwaiter struct{}

func (fakePersistConfirmationAwaiter) Register(string) (<-chan PersistConfirmation, error) {
	ch := make(chan PersistConfirmation)
	return ch, nil
}
func (fakePersistConfirmationAwaiter) Await(context.Context, string) (PersistConfirmation, error) {
	return PersistConfirmation{}, nil
}
func (fakePersistConfirmationAwaiter) Cancel(string) {}

var (
	_ ArtifactRequesterPort          = (*fakeArtifactRequester)(nil)
	_ AnalysisArtifactsPublisherPort = (*fakeAnalysisArtifactsPublisher)(nil)
	_ ArtifactsAwaiterPort           = (*fakeArtifactsAwaiter)(nil)
	_ PersistConfirmationAwaiterPort = (*fakePersistConfirmationAwaiter)(nil)
)

func TestPersistConfirmation_DiscriminatedUnion(t *testing.T) {
	t.Parallel()
	success := NewPersistConfirmationSuccess(&LegalAnalysisArtifactsPersisted{JobID: "j1"})
	if !success.IsSuccess() || success.IsFailure() {
		t.Errorf("Success-only must report IsSuccess=true, IsFailure=false")
	}

	failure := NewPersistConfirmationFailure(&LegalAnalysisArtifactsPersistFailed{JobID: "j1"})
	if failure.IsSuccess() || !failure.IsFailure() {
		t.Errorf("Failure-only must report IsSuccess=false, IsFailure=true")
	}

	empty := PersistConfirmation{}
	if empty.IsSuccess() || empty.IsFailure() {
		t.Errorf("empty must report IsSuccess=false and IsFailure=false")
	}

	// Both-set: ambiguous, neither helper should report a single outcome.
	// Constructed via direct literal to bypass the constructors' nil-guards
	// since these constructors prevent half-construction by design.
	both := PersistConfirmation{
		Success: &LegalAnalysisArtifactsPersisted{},
		Failure: &LegalAnalysisArtifactsPersistFailed{},
	}
	if both.IsSuccess() || both.IsFailure() {
		t.Errorf("both-set must report neither IsSuccess nor IsFailure (caller misuse)")
	}
}

func TestNewPersistConfirmation_NilEnvelopePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewPersistConfirmationSuccess(nil) must panic")
		}
	}()
	_ = NewPersistConfirmationSuccess(nil)
}

func TestNewPersistConfirmationFailure_NilEnvelopePanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewPersistConfirmationFailure(nil) must panic")
		}
	}()
	_ = NewPersistConfirmationFailure(nil)
}

func TestPersistConfirmation_IsValid_XOR(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		c    PersistConfirmation
		want bool
	}{
		{"success-only", NewPersistConfirmationSuccess(&LegalAnalysisArtifactsPersisted{}), true},
		{"failure-only", NewPersistConfirmationFailure(&LegalAnalysisArtifactsPersistFailed{}), true},
		{"empty", PersistConfirmation{}, false},
		{"both-set", PersistConfirmation{
			Success: &LegalAnalysisArtifactsPersisted{},
			Failure: &LegalAnalysisArtifactsPersistFailed{},
		}, false},
	}
	for _, tc := range cases {
		if got := tc.c.IsValid(); got != tc.want {
			t.Errorf("%s: IsValid() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
