package statustracker

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"contractpro/api-orchestrator/internal/egress/ssebroadcast"
	"contractpro/api-orchestrator/internal/infra/kvstore"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/consumer"
)

const idempotencyKeyPrefix = "lic-uncertain"

func idempotencyKey(versionID string) string {
	return idempotencyKeyPrefix + ":" + versionID
}

// confirmationMeta stores org/doc identity alongside the watchdog key so the
// watchdog (ORCH-TASK-042) can resolve version_id → org_id + doc_id for
// TimeoutAwaitingInput.
type confirmationMeta struct {
	OrganizationID string `json:"organization_id"`
	DocumentID     string `json:"document_id"`
	VersionID      string `json:"version_id"`
}

// handleLICClassificationUncertain processes lic.events.classification-uncertain.
//
// When LIC reports low classification confidence, this handler:
//  1. Validates required fields (organization_id, document_id, version_id)
//  2. Checks idempotency via Redis key lic-uncertain:{version_id} (fast-path)
//  3. Transitions the version to AWAITING_USER_INPUT via SetAwaitingUserInput
//  4. Stores confirmation metadata for the watchdog (ORCH-TASK-042)
//  5. Sets idempotency key (TTL 24h)
//  6. Broadcasts type_confirmation_required SSE event with classification payload
//
// Deduplication is two-layered: the idempotency key provides a fast-path skip
// for redeliveries, while the Lua-based SetAwaitingUserInput provides the true
// atomicity guarantee (only succeeds when status == ANALYZING).
func (t *Tracker) handleLICClassificationUncertain(ctx context.Context, e *consumer.LICClassificationUncertainEvent) error {
	if e.OrganizationID == "" || e.DocumentID == "" || e.VersionID == "" {
		t.log.Warn(ctx, "classification-uncertain missing identity fields, skipping")
		return nil
	}

	iKey := idempotencyKey(e.VersionID)
	_, err := t.kv.Get(ctx, iKey)
	if err == nil {
		t.log.Debug(ctx, "classification-uncertain already processed, skipping",
			"version_id", e.VersionID)
		return nil
	}
	if !errors.Is(err, kvstore.ErrKeyNotFound) {
		t.log.Error(ctx, "failed to check idempotency key",
			logger.ErrorAttr(err),
			"key", iKey)
		return err
	}

	if err := t.SetAwaitingUserInput(ctx, e.OrganizationID, e.DocumentID, e.VersionID); err != nil {
		if errors.Is(err, ErrInvalidTransition) {
			t.log.Warn(ctx, "classification-uncertain: invalid transition, skipping",
				"version_id", e.VersionID)
			return nil
		}
		return err
	}

	meta := confirmationMeta{
		OrganizationID: e.OrganizationID,
		DocumentID:     e.DocumentID,
		VersionID:      e.VersionID,
	}
	metaJSON, _ := json.Marshal(meta)
	if err := t.kv.Set(ctx, ConfirmationMetaKey(e.VersionID), string(metaJSON), t.confirmationTimeout); err != nil {
		t.log.Warn(ctx, "failed to store confirmation metadata",
			logger.ErrorAttr(err),
			"version_id", e.VersionID)
	}

	if err := t.kv.Set(ctx, iKey, "1", t.confirmationTimeout); err != nil {
		t.log.Warn(ctx, "failed to set idempotency key",
			logger.ErrorAttr(err),
			"key", iKey)
	}

	sseEvent := ssebroadcast.Event{
		EventType:     "type_confirmation_required",
		DocumentID:    e.DocumentID,
		VersionID:     e.VersionID,
		Status:        string(StatusAwaitingUserInput),
		Message:       statusMessages[StatusAwaitingUserInput],
		Timestamp:     t.now().UTC().Format(time.RFC3339),
		SuggestedType: e.SuggestedType,
		Confidence:    e.Confidence,
		Threshold:     e.Threshold,
		Alternatives:  convertAlternatives(e.Alternatives),
	}
	_ = t.broadcaster.Broadcast(ctx, e.OrganizationID, sseEvent)

	t.log.Info(ctx, "classification-uncertain processed, awaiting user input",
		"version_id", e.VersionID,
		"suggested_type", e.SuggestedType,
		"confidence", e.Confidence,
		"threshold", e.Threshold)

	return nil
}

func convertAlternatives(alts []consumer.ClassificationAlternative) []ssebroadcast.ClassificationAlternative {
	if len(alts) == 0 {
		return nil
	}
	out := make([]ssebroadcast.ClassificationAlternative, len(alts))
	for i, a := range alts {
		out[i] = ssebroadcast.ClassificationAlternative{
			ContractType: a.ContractType,
			Confidence:   a.Confidence,
		}
	}
	return out
}
