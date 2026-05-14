package port

import (
	"context"
	"testing"
)

type fakeProviderRouter struct{}

func (fakeProviderRouter) Complete(context.Context, CompletionRequest) (PrimaryCallResult, error) {
	return PrimaryCallResult{UsedProvider: ProviderClaude}, nil
}
func (fakeProviderRouter) CompleteRepair(
	context.Context, CompletionRequest, LLMProviderID,
) (CompletionResponse, error) {
	return CompletionResponse{}, nil
}

var _ ProviderRouterPort = (*fakeProviderRouter)(nil)

// TestProviderRouter_PrimaryCallResultCarriesUsedProvider asserts the sticky-
// provider invariant: Complete returns the provider that actually served the
// request, so the caller can pass it back into CompleteRepair (high-architecture
// §6.8, OQ-10).
func TestProviderRouter_PrimaryCallResultCarriesUsedProvider(t *testing.T) {
	t.Parallel()
	var r ProviderRouterPort = fakeProviderRouter{}
	res, err := r.Complete(context.Background(), CompletionRequest{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !res.UsedProvider.IsKnown() {
		t.Fatalf("UsedProvider must be a known provider; got %q", res.UsedProvider)
	}

	if _, err := r.CompleteRepair(context.Background(), CompletionRequest{}, res.UsedProvider); err != nil {
		t.Fatalf("CompleteRepair: %v", err)
	}
}
