package port

import (
	"context"
	"testing"
)

// Compile-time assertions that the inbound handler interfaces are
// implementable. Each fake implements exactly one method per port —
// the var _ Port = (*fake)(nil) lines below are the actual checks.

type fakeVersionArtifactsReadyHandler struct{}

func (fakeVersionArtifactsReadyHandler) HandleVersionArtifactsReady(
	context.Context, VersionProcessingArtifactsReady,
) error {
	return nil
}

type fakeVersionCreatedHandler struct{}

func (fakeVersionCreatedHandler) HandleVersionCreated(
	context.Context, VersionCreated,
) error {
	return nil
}

type fakeArtifactsProvidedHandler struct{}

func (fakeArtifactsProvidedHandler) HandleArtifactsProvided(
	context.Context, ArtifactsProvided,
) error {
	return nil
}

type fakePersistConfirmationHandler struct{}

func (fakePersistConfirmationHandler) HandlePersisted(
	context.Context, LegalAnalysisArtifactsPersisted,
) error {
	return nil
}
func (fakePersistConfirmationHandler) HandlePersistFailed(
	context.Context, LegalAnalysisArtifactsPersistFailed,
) error {
	return nil
}

type fakeUserConfirmedTypeHandler struct{}

func (fakeUserConfirmedTypeHandler) HandleUserConfirmedType(
	context.Context, UserConfirmedType,
) error {
	return nil
}

var (
	_ VersionArtifactsReadyHandler = (*fakeVersionArtifactsReadyHandler)(nil)
	_ VersionCreatedHandler        = (*fakeVersionCreatedHandler)(nil)
	_ ArtifactsProvidedHandler     = (*fakeArtifactsProvidedHandler)(nil)
	_ PersistConfirmationHandler   = (*fakePersistConfirmationHandler)(nil)
	_ UserConfirmedTypeHandler     = (*fakeUserConfirmedTypeHandler)(nil)
)

// TestInboundHandlers_CallableThroughInterface is a smoke test exercising
// the dispatch through the interface, not just the compile-time check —
// catches regressions where the interface signature is changed but the
// fake still satisfies it accidentally.
func TestInboundHandlers_CallableThroughInterface(t *testing.T) {
	t.Parallel()
	var h VersionArtifactsReadyHandler = fakeVersionArtifactsReadyHandler{}
	if err := h.HandleVersionArtifactsReady(context.Background(), VersionProcessingArtifactsReady{}); err != nil {
		t.Fatalf("HandleVersionArtifactsReady: %v", err)
	}

	var pc PersistConfirmationHandler = fakePersistConfirmationHandler{}
	if err := pc.HandlePersisted(context.Background(), LegalAnalysisArtifactsPersisted{}); err != nil {
		t.Fatalf("HandlePersisted: %v", err)
	}
	if err := pc.HandlePersistFailed(context.Background(), LegalAnalysisArtifactsPersistFailed{}); err != nil {
		t.Fatalf("HandlePersistFailed: %v", err)
	}
}
