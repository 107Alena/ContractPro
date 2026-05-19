package router

import (
	"context"
	"encoding/json"
	"errors"

	"contractpro/legal-intelligence-core/internal/application/pipeline"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// RouteVersionArtifactsReady is the §6.5:624-634 restart-decision-tree entry
// point (build-spec D4). It guards lic-trigger:{version_id} via the 4-status
// IdempotencyGuard, drives the pipeline on a fresh acquisition, and resolves
// every {PROCESSING, PAUSED, COMPLETED, transport-down} branch the
// high-architecture restart tree enumerates. See routeVersionArtifactsReady
// for the full algorithm.
func (r *Router) RouteVersionArtifactsReady(ctx context.Context, evt port.VersionProcessingArtifactsReady) error {
	ctx, span := r.tracer.StartRoute(ctx, "dm.events.version-artifacts-ready")
	err := r.routeVersionArtifactsReady(ctx, evt)
	span.Finish(err)
	return err
}

// RouteVersionCreated is the lic-version-meta cache populator entry point
// (build-spec D5). It guards lic-version-created:{version_id} via the
// 2-status IdempotencyGuard, then writes the per-version cache payload
// (parent_version_id + origin_type) consumed by the orchestrator's
// resolveParentAndMode fallback (orchestrator.go:765-777 — 036 DEFECT-1).
// Every failure path degrades silently (ACK + WARN) per 036 DEFECT-1: the
// trigger's ParentVersionID is the PRIMARY RE_CHECK signal; the cache is a
// fallback whose absence degrades to INITIAL, never FAILED.
func (r *Router) RouteVersionCreated(ctx context.Context, evt port.VersionCreated) error {
	ctx, span := r.tracer.StartRoute(ctx, "dm.events.version-created")
	err := r.routeVersionCreated(ctx, evt)
	span.Finish(err)
	return err
}

// RouteArtifactsProvided routes the async DM artifacts response to the
// in-process awaiter slot the pipeline goroutine is blocked on (build-spec
// D6). It guards lic-artifacts-resp:{correlation_id} via the 2-status
// IdempotencyGuard, then Delivers to ArtifactsAwaiterDeliverer. Awaiter
// registry-miss (slot timed out + Cancel'd) ⇒ silent ACK (the pipeline will
// publish FAILED{DM_ARTIFACTS_TIMEOUT} itself, orchestrator.go:826).
func (r *Router) RouteArtifactsProvided(ctx context.Context, evt port.ArtifactsProvided) error {
	ctx, span := r.tracer.StartRoute(ctx, "dm.responses.artifacts-provided")
	err := r.routeArtifactsProvided(ctx, evt)
	span.Finish(err)
	return err
}

// RoutePersisted routes the async DM persist-success confirmation to the
// awaiter slot the pipeline goroutine is blocked on (build-spec D7). It
// guards lic-persist-resp:{job_id} via the 2-status IdempotencyGuard, builds
// the PersistConfirmation success envelope via NewPersistConfirmationSuccess,
// and Delivers to PersistConfirmationDeliverer.
func (r *Router) RoutePersisted(ctx context.Context, evt port.LegalAnalysisArtifactsPersisted) error {
	ctx, span := r.tracer.StartRoute(ctx, "dm.responses.lic-artifacts-persisted")
	err := r.routePersisted(ctx, evt)
	span.Finish(err)
	return err
}

// RoutePersistFailed routes the async DM persist-failure confirmation to the
// awaiter slot (build-spec D7). It guards lic-persist-fail:{job_id} via the
// 2-status IdempotencyGuard, builds the PersistConfirmation failure envelope
// via NewPersistConfirmationFailure, and Delivers to
// PersistConfirmationDeliverer. The Router does NOT inspect evt.IsRetryable
// here — the pipeline's awaitPersist (orchestrator.go:1017-1046) discriminates
// success vs failure on the awaiter side.
func (r *Router) RoutePersistFailed(ctx context.Context, evt port.LegalAnalysisArtifactsPersistFailed) error {
	ctx, span := r.tracer.StartRoute(ctx, "dm.responses.lic-artifacts-persist-failed")
	err := r.routePersistFailed(ctx, evt)
	span.Finish(err)
	return err
}

// RouteUserConfirmedType is the orchestrator-resume entry point (build-spec
// D8/R4). The Manager (037) OWNS the SETNX lic-user-confirmed:{version_id}
// guard, contract_type validation, the §11.2 tenant check, and DLQ
// publication for invalid-format / invalid-whitelist / tenant-mismatch
// (manager.go:317,384). The Router transparently delegates and maps
// {nil ⇒ ACK, retryable err ⇒ NACK, non-retryable err ⇒ ACK} — non-retryable
// is ACK (NOT DLQ) because Manager has either already DLQ-published or
// deliberately not (the corrupt-stored-blob case manager.go:399-401).
func (r *Router) RouteUserConfirmedType(ctx context.Context, evt port.UserConfirmedType) error {
	ctx, span := r.tracer.StartRoute(ctx, "orch.commands.user-confirmed-type")
	err := r.routeUserConfirmedType(ctx, evt)
	span.Finish(err)
	return err
}

// ---------------------------------------------------------------------------
// Private per-topic flow bodies (build-spec D4..D8).
// ---------------------------------------------------------------------------

// routeVersionArtifactsReady implements the §6.5:624-634 restart-decision-tree
// (build-spec D4). The full algorithm is the body's structure:
//
//  1. CheckAndAcquire(lic-trigger) — single atomic Lua (idempotency D3.2/D4).
//  2. Transport error ⇒ wrap as *DomainError{IDEMPOTENCY_STORE_UNAVAILABLE,
//     retryable=true} (R1 — Router owns the model-mapping; Guard returns
//     kvstore err verbatim per BUILD_SPEC_LIC_038 R4). Non-publishable code
//     so Consumer Nack(false)→DLX, no FAILED on the wire.
//  3. (Absent,false,nil) — acquired, own the slot:
//     - StartHeartbeat (defer stopHB() IMMEDIATELY so a panic in Run still
//     stops the heartbeat — sync.Once-guarded, twice-safe).
//     - Run the pipeline.
//     - pipeline.IsPaused(err) FIRST (D4 step 3c — Pin 9 intact): ACK
//     without SetCompleted (037 already SetPaused).
//     - nil ⇒ SetCompleted(lic-trigger, CompletedTTL); ACK.
//     - *DomainError ⇒ SetCompleted (terminal); return verbatim (Consumer
//     maps via model.IsRetryable; pipeline already published FAILED).
//  4. (status,true,nil) — present:
//     - PROCESSING ⇒ retryable IDEMPOTENCY_STORE_UNAVAIL (non-publishable;
//     the in-flight run will publish its own terminal status).
//     - PAUSED ⇒ Load lic-pending-state:
//     · ErrPendingStateNotFound ⇒ §6.5:631 + R3: publish FAILED{USER_
//     CONFIRMATION_EXPIRED} + SetCompleted(lic-trigger,
//     PendingStateTTL) + ACK. NO DLQ.
//     · Other Load error ⇒ retryable IDEMPOTENCY_STORE_UNAVAIL.
//     · hit ⇒ Manager.RepublishPauseEvents (§6.5:631 republish);
//     return verbatim (nil ACK | retryable NACK).
//     - COMPLETED ⇒ ACK (§6.5:632 "ACK без обработки").
//     - default (defensive — parseStatus maps unknown → PROCESSING) ⇒
//     retryable IDEMPOTENCY_STORE_UNAVAIL.
func (r *Router) routeVersionArtifactsReady(ctx context.Context, evt port.VersionProcessingArtifactsReady) error {
	key := keyTrigger(evt.VersionID)

	// (1) Atomic SETNX-or-read-existing.
	status, alreadyExists, gErr := r.idem.CheckAndAcquire(ctx, key, r.cfg.ProcessingTTL)

	// (2) Transport error (FallbackEnabled=false on the Guard).
	if gErr != nil {
		// IDEMPOTENCY_STORE_UNAVAILABLE is non-publishable (empty
		// UserMessage, error_codes.go:228) — Consumer Nack(false)→DLX,
		// pipeline never reached, no FAILED on the wire.
		return model.NewDomainError(model.ErrCodeIdempotencyStoreUnavail, model.StageReceived).
			WithCause(gErr).
			WithDevMessage("router: CheckAndAcquire(lic-trigger) failed; NACK→retry-DLX")
	}

	// (3) Acquired, own the slot.
	if !alreadyExists {
		// (3a) Start heartbeat IMMEDIATELY; defer stopHB() so a panic
		// inside Run still stops the heartbeat. heartbeat.go:53 sync.Once
		// — twice-safe.
		stopHB := r.idem.StartHeartbeat(ctx, key, r.cfg.ProcessingTTL)
		defer stopHB()

		// (3b) Drive the pipeline (036 owns Acquire/timeout/publishFailed).
		runErr := r.pipelineRunner.Run(ctx, evt)

		// (3c) Outcome routing — pipeline.IsPaused BEFORE model.IsRetryable
		// (pipeline/CLAUDE.md "Pin 9 intact").
		if pipeline.IsPaused(runErr) {
			// 037 already SetPaused(lic-trigger). DO NOT SetCompleted.
			// ACK the source: return nil.
			return nil
		}
		if runErr == nil {
			// Terminal success — finalize lic-trigger=COMPLETED 24h.
			if cErr := r.idem.SetCompleted(ctx, key, r.cfg.CompletedTTL); cErr != nil {
				// Log-and-continue: the pipeline is COMPLETED on the wire;
				// failing to flip lic-trigger only orphans a PROCESSING
				// key whose 150s TTL reconciles naturally.
				r.log.Error(ctx,
					"router: SetCompleted(lic-trigger) failed after pipeline COMPLETED; orphan PROCESSING, TTL reconciles",
					"version_id", evt.VersionID, "cause", cErr)
			}
			return nil // ACK
		}
		// runErr is a *model.DomainError — pipeline ALREADY published
		// FAILED. Finalize lic-trigger=COMPLETED 24h (terminal state,
		// not a re-run path). Return the pipeline's error verbatim —
		// Consumer maps via model.IsRetryable on the broker boundary.
		if cErr := r.idem.SetCompleted(ctx, key, r.cfg.CompletedTTL); cErr != nil {
			r.log.Error(ctx,
				"router: SetCompleted(lic-trigger) failed after pipeline FAILED; orphan, TTL reconciles",
				"version_id", evt.VersionID, "cause", cErr)
		}
		return runErr
	}

	// (4) Present — branch on existing status.
	switch status {
	case port.IdempotencyProcessing:
		// (4a) Concurrent in-flight run. §6.5:629 "ждать или NACK для
		// повтора"; redelivery hits the same key, may then see COMPLETED.
		// Non-publishable code so no FAILED on the wire (the in-flight
		// run will publish its own terminal status).
		return model.NewDomainError(model.ErrCodeIdempotencyStoreUnavail, model.StageReceived).
			WithRetryable(true).
			WithDevMessage("router: lic-trigger=PROCESSING — concurrent in-flight pipeline; NACK→retry-DLX")

	case port.IdempotencyPaused:
		// (4b) PAUSED — §6.5:631 safety-net. Load lic-pending-state.
		ptc, lErr := r.pendingStateLoader.Load(ctx, evt.VersionID)
		if lErr != nil {
			if errors.Is(lErr, port.ErrPendingStateNotFound) {
				// §6.5:631 + R3 "PENDING_STATE_LOST"-equivalent →
				// USER_CONFIRMATION_EXPIRED. Publish FAILED via Router's
				// statusPub (pipeline never ran for this delivery, so its
				// publishFailed cannot fire); then SetCompleted lic-trigger
				// 24h (§6.10:782 — closes the slot; redelivery sees
				// COMPLETED ⇒ ACK without work). ACK (the message is
				// terminal); NO DLQ (expired pause is not poison — 037 R5
				// precedent manager.go:360-364).
				r.publishFailedTerminal(ctx, evt, model.ErrCodeUserConfirmationExpired)
				r.setCompletedSafe(ctx, key, evt.VersionID)
				return nil // ACK
			}
			// Other Load error (Redis transient on lic-pending-state) —
			// NACK→retry-DLX. PendingStatePort is Redis-backed but goes
			// through a separate adapter (047 — pendingstate package); a
			// transient there mirrors the lic-trigger transient (R1):
			// non-publishable IDEMPOTENCY_STORE_UNAVAILABLE retryable.
			return model.NewDomainError(model.ErrCodeIdempotencyStoreUnavail, model.StageReceived).
				WithRetryable(true).
				WithCause(lErr).
				WithDevMessage("router: lic-pending-state Load failed; NACK→retry-DLX")
		}
		// PAUSED + pending-state present — §6.5:631 republish only.
		// Stage 1 NOT restarted.
		if rErr := r.pendingMgr.RepublishPauseEvents(ctx, ptc); rErr != nil {
			// Manager returns *model.DomainError (manager.go:283-294 —
			// always retryable INTERNAL_ERROR on republish failure).
			// Consumer NACK→retry-DLX.
			return rErr
		}
		return nil // ACK

	case port.IdempotencyCompleted:
		// (4c) Already done — §6.5:632 "ACK без обработки".
		return nil

	default:
		// (4d) Defensive: parseStatus maps unknown → PROCESSING
		// (idempotency D5). This case is unreachable; mirror PROCESSING
		// handling (retryable NACK).
		return model.NewDomainError(model.ErrCodeIdempotencyStoreUnavail, model.StageReceived).
			WithRetryable(true).
			WithDevMessage("router: lic-trigger unexpected status; NACK→retry-DLX")
	}
}

// routeVersionCreated implements the lic-version-meta cache populator
// (build-spec D5). The cache is the FALLBACK for the §8.3 race where
// VersionProcessingArtifactsReady arrives without parent_version_id but a
// VersionCreated for the same version has already populated the cache (036
// DEFECT-1 — orchestrator.go:765-777). Every failure degrades silently per
// DEFECT-1: trigger.ParentVersionID is PRIMARY; cache miss/error ⇒ degrade
// to INITIAL, NEVER fails.
func (r *Router) routeVersionCreated(ctx context.Context, evt port.VersionCreated) error {
	key := keyVersionCreated(evt.VersionID)

	// 2-status guard (no heartbeat, no PAUSED branch — the write is a
	// fire-once cache populator). Per-call ttl = CompletedTTL (24h).
	status, alreadyExists, gErr := r.idem.CheckAndAcquire(ctx, key, r.cfg.CompletedTTL)

	if gErr != nil {
		// Transport error. The cache is a degradation-fallback (DEFECT-1):
		// silently ACK so the cache stays empty for this version_id —
		// pipeline will degrade to INITIAL if a later RE_CHECK arrives
		// without ParentVersionID on the trigger. WARN log only.
		r.log.Warn(ctx,
			"router: CheckAndAcquire(lic-version-created) Redis-down; cache write skipped (pipeline degrades to INITIAL if trigger lacks parent_version_id)",
			"version_id", evt.VersionID, "cause", gErr)
		return nil // ACK — defensible degrade per 036 DEFECT-1
	}

	if !alreadyExists {
		// First sighting — write the meta payload, then flip status to
		// COMPLETED.
		payload, jErr := json.Marshal(struct {
			ParentVersionID *string `json:"parent_version_id,omitempty"`
			OriginType      string  `json:"origin_type,omitempty"`
		}{
			ParentVersionID: evt.ParentVersionID,
			OriginType:      evt.OriginType,
		})
		if jErr != nil {
			// Cannot happen for this simple struct (no unmarshalable
			// fields) — log + ACK (cache miss degrade). Defensive: never
			// NACK on a local marshal defect; it would loop forever.
			r.log.Error(ctx,
				"router: VersionCreated payload marshal failed; cache write skipped",
				"version_id", evt.VersionID, "cause", jErr)
			return nil
		}
		if wErr := r.versionMetaWriter.Set(ctx, evt.VersionID, payload, r.cfg.MetaCacheTTL); wErr != nil {
			// Cache write failed — degrade silently (DEFECT-1). The
			// lic-version-created PROCESSING marker stays for 24h; a
			// redelivery would re-enter this branch and try again. We do
			// NOT SetCompleted here — leave the slot "PROCESSING" so a
			// redelivery retries the Set (the cache value, not the
			// PROCESSING flag, is what matters).
			r.log.Warn(ctx,
				"router: lic-version-meta Set failed; redelivery will retry",
				"version_id", evt.VersionID, "cause", wErr)
			return nil // ACK — degrade per 036 DEFECT-1
		}
		if cErr := r.idem.SetCompleted(ctx, key, r.cfg.CompletedTTL); cErr != nil {
			r.log.Error(ctx,
				"router: SetCompleted(lic-version-created) failed after meta write; orphan PROCESSING, TTL reconciles",
				"version_id", evt.VersionID, "cause", cErr)
		}
		return nil
	}

	// alreadyExists — duplicate. The §6.3 2-status semantics: any
	// non-Absent ⇒ ACK (the cache was already populated on the original
	// delivery). PROCESSING/PAUSED for a 2-status key is defensive
	// (idempotency D5 parseStatus garbage → PROCESSING); treat as
	// stale-in-flight or completed-but-mid-transition — ACK either way.
	_ = status
	return nil
}

// routeArtifactsProvided implements DM artifacts response routing (build-spec
// D6). It guards lic-artifacts-resp:{correlation_id} via the 2-status
// IdempotencyGuard, then Delivers to the in-process awaiter slot the pipeline
// goroutine is blocked on. Awaiter registry-miss (slot timed out + Cancel'd)
// ⇒ silent ACK + WARN (the pipeline will publish FAILED{DM_ARTIFACTS_TIMEOUT}
// itself, orchestrator.go:826).
func (r *Router) routeArtifactsProvided(ctx context.Context, evt port.ArtifactsProvided) error {
	key := keyArtifactsResp(evt.CorrelationID)

	status, alreadyExists, gErr := r.idem.CheckAndAcquire(ctx, key, r.cfg.CompletedTTL)

	if gErr != nil {
		// Transport-class. Safer than NACK-looping (the pipeline goroutine
		// would time out at DM_REQUEST_TIMEOUT regardless): degrade to ACK
		// + best-effort Deliver — the awaiter may already be cancelled,
		// in which case Deliver is a noop on the 041 side.
		r.log.Warn(ctx,
			"router: CheckAndAcquire(lic-artifacts-resp) Redis-down; best-effort Deliver, ACK",
			"correlation_id", evt.CorrelationID, "cause", gErr)
		_ = r.artifactDeliverer.Deliver(evt.CorrelationID, evt)
		return nil
	}

	if !alreadyExists {
		if dErr := r.artifactDeliverer.Deliver(evt.CorrelationID, evt); dErr != nil {
			// Awaiter registry-miss (slot timed out + Cancel'd) or other
			// local error. The response is dead-letter material: silently
			// ACK + WARN.
			r.log.Warn(ctx,
				"router: Deliver(ArtifactsProvided) registry-miss/timeout; ACK silently",
				"correlation_id", evt.CorrelationID, "cause", dErr)
			return nil
		}
		if cErr := r.idem.SetCompleted(ctx, key, r.cfg.CompletedTTL); cErr != nil {
			r.log.Error(ctx,
				"router: SetCompleted(lic-artifacts-resp) failed; orphan, TTL reconciles",
				"correlation_id", evt.CorrelationID, "cause", cErr)
		}
		return nil
	}

	// alreadyExists — duplicate response. ACK silently; the awaiter has
	// either already received or moved past (single-receiver slot
	// semantics).
	_ = status
	return nil
}

// routePersisted implements DM persist-success confirmation routing (build-
// spec D7). It guards lic-persist-resp:{job_id} via the 2-status
// IdempotencyGuard, builds a PersistConfirmation success envelope via
// port.NewPersistConfirmationSuccess (dm.go:138 — panics on nil; the Router
// always passes &evt so the precondition is structurally satisfied), and
// Delivers to PersistConfirmationDeliverer.
func (r *Router) routePersisted(ctx context.Context, evt port.LegalAnalysisArtifactsPersisted) error {
	key := keyPersistResp(evt.JobID)

	status, alreadyExists, gErr := r.idem.CheckAndAcquire(ctx, key, r.cfg.CompletedTTL)

	if gErr != nil {
		r.log.Warn(ctx,
			"router: CheckAndAcquire(lic-persist-resp) Redis-down; best-effort Deliver, ACK",
			"job_id", evt.JobID, "cause", gErr)
		_ = r.persistDeliverer.Deliver(evt.JobID, port.NewPersistConfirmationSuccess(&evt))
		return nil
	}

	if !alreadyExists {
		if dErr := r.persistDeliverer.Deliver(evt.JobID, port.NewPersistConfirmationSuccess(&evt)); dErr != nil {
			r.log.Warn(ctx,
				"router: Deliver(Persisted) registry-miss/timeout; ACK silently",
				"job_id", evt.JobID, "cause", dErr)
			return nil
		}
		if cErr := r.idem.SetCompleted(ctx, key, r.cfg.CompletedTTL); cErr != nil {
			r.log.Error(ctx,
				"router: SetCompleted(lic-persist-resp) failed; orphan, TTL reconciles",
				"job_id", evt.JobID, "cause", cErr)
		}
		return nil
	}

	_ = status
	return nil
}

// routePersistFailed implements DM persist-failure confirmation routing
// (build-spec D7). It guards lic-persist-fail:{job_id} via the 2-status
// IdempotencyGuard, builds a PersistConfirmation failure envelope via
// port.NewPersistConfirmationFailure, and Delivers to
// PersistConfirmationDeliverer. The Router does NOT inspect evt.IsRetryable
// here — the orchestrator's awaitPersist classifies failure (DM_PERSIST_-
// FAILED with conf.Failure.IsRetryable flowing through to the response code,
// orchestrator.go:1037-1042). Two distinct keys (lic-persist-resp vs
// lic-persist-fail) because §6.3 separates the two topics into two
// idempotency surfaces.
func (r *Router) routePersistFailed(ctx context.Context, evt port.LegalAnalysisArtifactsPersistFailed) error {
	key := keyPersistFail(evt.JobID)

	status, alreadyExists, gErr := r.idem.CheckAndAcquire(ctx, key, r.cfg.CompletedTTL)

	if gErr != nil {
		r.log.Warn(ctx,
			"router: CheckAndAcquire(lic-persist-fail) Redis-down; best-effort Deliver, ACK",
			"job_id", evt.JobID, "cause", gErr)
		_ = r.persistDeliverer.Deliver(evt.JobID, port.NewPersistConfirmationFailure(&evt))
		return nil
	}

	if !alreadyExists {
		if dErr := r.persistDeliverer.Deliver(evt.JobID, port.NewPersistConfirmationFailure(&evt)); dErr != nil {
			r.log.Warn(ctx,
				"router: Deliver(PersistFailed) registry-miss/timeout; ACK silently",
				"job_id", evt.JobID, "cause", dErr)
			return nil
		}
		if cErr := r.idem.SetCompleted(ctx, key, r.cfg.CompletedTTL); cErr != nil {
			r.log.Error(ctx,
				"router: SetCompleted(lic-persist-fail) failed; orphan, TTL reconciles",
				"job_id", evt.JobID, "cause", cErr)
		}
		return nil
	}

	_ = status
	return nil
}

// routeUserConfirmedType delegates to the Manager (build-spec D8/R4). The
// Manager OWNS the SETNX lic-user-confirmed:{version_id} guard, contract_type
// validation (regex + 12-whitelist), the §11.2 tenant check, and DLQ
// publication for invalid-format / invalid-whitelist / tenant-mismatch
// (manager.go:317,384). The Router maps the return:
//
//	nil            ⇒ ACK
//	retryable err  ⇒ return err verbatim (Consumer Nack→retry-DLX)
//	non-retryable  ⇒ ACK (Manager has either already DLQ-published or
//	                 deliberately not — corrupt-stored-blob case
//	                 manager.go:399-401; the Router NEVER republishes DLQ).
func (r *Router) routeUserConfirmedType(ctx context.Context, evt port.UserConfirmedType) error {
	err := r.pendingMgr.HandleUserConfirmedType(ctx, evt)
	if err == nil {
		return nil // ACK
	}
	if model.IsRetryable(err) {
		return err // NACK→retry-DLX
	}
	// Non-retryable: ACK so the message does not loop. The Manager has
	// already taken its terminal decision (DLQ for poison wire messages,
	// or deliberately not for corrupt stored blob — R4).
	r.log.Warn(ctx,
		"router: UserConfirmedType non-retryable; ACK silently (Manager owns DLQ for poison cases)",
		"version_id", evt.VersionID, "cause", err)
	return nil
}
