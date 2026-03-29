# Application Layer — CLAUDE.md

Orchestrators and cross-cutting application services. Implements inbound port handlers and coordinates components.

## Orchestrators

**processing/** — Processing Pipeline Orchestrator (implements ProcessingCommandHandler)
- Pipeline stages: validate → fetch → OCR → extract text → extract structure → build tree → send to DM → await confirmation → publish completion
- Handles retries on transient errors (domain/port.DomainError.Retryable)
- On terminal failure: sends to DLQ, publishes ProcessingFailed event
- Manages job lifecycle: updates status, logs stages, collects warnings
- Uses LifecycleManager for state management and ConcurrencyLimiterPort for job rate limiting

**comparison/** — Comparison Pipeline Orchestrator (implements ComparisonCommandHandler)
- Pipeline: request semantic trees from DM → await both trees → compare → send diff to DM → await confirmation → publish completion
- Uses PendingResponseRegistryPort to correlate async DM tree responses with pipeline context
- Handles timeouts and missing responses
- Publishes ComparisonFailed if trees unavailable or comparison error
- Coordinates with DMTreeRequesterPort and DMConfirmationAwaiterPort

## Services

**lifecycle/** — LifecycleManager
- Manages job status transitions with validation (status.go defines valid paths)
- Publishes domain events (StatusChanged, ProcessingCompleted, etc.)
- Enforces timeout constraints (120s default, configurable)
- Cleanup operations on terminal states
- Thread-safe for concurrent pipeline orchestrators

**dmconfirmation/** — DM Confirmation Awaiter (implements DMConfirmationAwaiterPort)
- Tracks pending DM persistence confirmations during processing/comparison
- Register(jobID) before sending artifacts to DM
- Await(jobID, timeout) blocks until Confirm/Reject
- Handles timeouts and async responses from DM
- Paired with PendingResponseRegistryPort

**pendingresponse/** — Pending Response Registry (implements PendingResponseRegistryPort)
- Tracks and correlates async DM responses (tree responses, confirmations) to job context
- Register before outbound request, then Store/Retrieve/Remove responses
- Thread-safe multi-reader pattern for comparison pipeline
- Cleans up on job terminal state

## Patterns

- NewComponentName() constructors
- Pipeline uses helper functions for each stage (not full orchestration class)
- Error handling: retryable → retry, non-retryable → DLQ + event
- Logging and warnings collected via domain/port interfaces
