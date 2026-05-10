# Sequence Diagrams — Legal Intelligence Core

Mermaid-диаграммы для всех сценариев работы LIC. Соответствуют сценариям, описанным в `high-architecture.md` §8.

---

## 1. Happy path — анализ INITIAL версии договора

Соответствует `high-architecture.md` §8.1.

```mermaid
sequenceDiagram
    autonumber
    participant DM as Document Management
    participant LIC as LIC Service
    participant Redis as Redis (idempotency, pending)
    participant LLM as LLM Provider Router\n(Claude / OpenAI / Gemini)
    participant Orch as Orchestrator

    DM->>LIC: dm.events.version-artifacts-ready
    LIC->>Redis: SETNX lic-trigger:{version_id} PROCESSING (ttl 90s)
    LIC->>Orch: lic.events.status-changed\n{status:IN_PROGRESS, stage:STAGE_REQUESTING_ARTIFACTS}
    LIC->>DM: lic.requests.artifacts\n[SEMANTIC_TREE, EXTRACTED_TEXT,\nDOCUMENT_STRUCTURE, PROCESSING_WARNINGS]
    DM-->>LIC: dm.responses.artifacts-provided
    Note over LIC: Stage 1 (parallel)
    par
        LIC->>LLM: TypeClassifier
        LLM-->>LIC: ClassificationResult\n(confidence ≥ threshold)
    and
        LIC->>LLM: KeyParamsExtractor
        LLM-->>LIC: KeyParameters
    end
    LIC->>LLM: PartyConsistency (Stage 2)
    LLM-->>LIC: PartyConsistencyFindings
    Note over LIC: Stage 3 (parallel)
    par
        LIC->>LLM: MandatoryConditions
        LLM-->>LIC: MandatoryConditionsReport
    and
        LIC->>LLM: RiskDetection
        LLM-->>LIC: RiskAnalysis
    end
    LIC->>LLM: Recommendation (Stage 4)
    LLM-->>LIC: Recommendations
    Note over LIC: Stage 5 (parallel)
    par
        LIC->>LLM: BusinessSummary
        LLM-->>LIC: Summary
    and
        LIC->>LLM: DetailedReport
        LLM-->>LIC: DetailedReport
    end
    Note over LIC: Deterministic calc
    LIC->>LIC: RISK_PROFILE + AGGREGATE_SCORE
    LIC->>DM: lic.artifacts.analysis-ready\n(LegalAnalysisArtifactsReady)
    DM-->>LIC: dm.responses.lic-artifacts-persisted
    LIC->>Orch: lic.events.status-changed\n{status:COMPLETED}
    LIC->>Redis: SET lic-trigger:{version_id} COMPLETED (ttl 24h)
    Note over LIC: ACK исходного сообщения
```

---

## 2. Низкая уверенность классификации (FR-2.1.3)

Соответствует `high-architecture.md` §8.2.

### 2.1 Pause на user confirmation

```mermaid
sequenceDiagram
    autonumber
    participant DM as Document Management
    participant LIC as LIC Service
    participant Redis as Redis
    participant LLM as LLM Provider Router
    participant Orch as Orchestrator

    DM->>LIC: dm.events.version-artifacts-ready
    LIC->>Redis: SETNX lic-trigger:{version_id} PROCESSING
    LIC->>Orch: lic.events.status-changed (IN_PROGRESS)
    LIC->>DM: lic.requests.artifacts
    DM-->>LIC: dm.responses.artifacts-provided
    par Stage 1 (parallel)
        LIC->>LLM: TypeClassifier
        LLM-->>LIC: ClassificationResult\n(confidence=0.62 < 0.75)
    and
        LIC->>LLM: KeyParamsExtractor
        LLM-->>LIC: KeyParameters
    end
    Note over LIC: confidence < threshold → pause
    LIC->>Redis: SET lic-pending-state:{version_id}\n(serialized state, ttl 25h)
    LIC->>Orch: lic.events.classification-uncertain\n{suggested_type, confidence, threshold, alternatives}
    LIC->>Orch: lic.events.status-changed\n{status:IN_PROGRESS, stage:STAGE_AWAITING_USER_CONFIRMATION}
    LIC->>Redis: SET lic-trigger:{version_id} COMPLETED (ttl 24h)
    Note over LIC: ACK исходного сообщения\n(не держим long-running consumer)
```

### 2.2 Resume после UserConfirmedType

```mermaid
sequenceDiagram
    autonumber
    participant Orch as Orchestrator
    participant LIC as LIC Service
    participant Redis as Redis
    participant LLM as LLM Provider Router
    participant DM as Document Management

    Orch->>LIC: orch.commands.user-confirmed-type\n{contract_type:"SUPPLY"}
    LIC->>Redis: SETNX lic-user-confirmed:{version_id} PROCESSING
    LIC->>LIC: Validate contract_type against whitelist
    LIC->>Redis: GET lic-pending-state:{version_id}
    Note over LIC: Override ClassificationResult.contract_type\nclassification.confidence = 1.0
    LIC->>Orch: lic.events.status-changed (IN_PROGRESS)
    Note over LIC: Stage 2 — Stage 5 как в §1
    LIC->>LLM: PartyConsistency
    LIC->>LLM: ... (и далее цепочка)
    LIC->>DM: lic.artifacts.analysis-ready
    DM-->>LIC: dm.responses.lic-artifacts-persisted
    LIC->>Orch: lic.events.status-changed (COMPLETED)
    LIC->>Redis: SET lic-user-confirmed:{version_id} COMPLETED (ttl 24h)
    Note over LIC: ACK
```

### 2.3 TTL expired до UserConfirmedType

```mermaid
sequenceDiagram
    autonumber
    participant Orch as Orchestrator
    participant LIC as LIC Service
    participant Redis as Redis

    Note over Redis: lic-pending-state:{version_id}\nexpires after 25h
    Orch->>LIC: orch.commands.user-confirmed-type\n(приходит спустя 26 часов)
    LIC->>Redis: SETNX lic-user-confirmed:{version_id}
    LIC->>Redis: GET lic-pending-state:{version_id}
    Redis-->>LIC: nil (expired)
    LIC->>Orch: lic.events.status-changed\n{status:FAILED, error_code:USER_CONFIRMATION_EXPIRED, is_retryable:false,\n error_message:"Время на подтверждение типа договора истекло. Запустите проверку заново."}
    Note over LIC: ACK сообщения, audit-лог
```

> Прим.: Orchestrator-watchdog (TTL 24h) обычно срабатывает раньше LIC TTL (25h, см. ASSUMPTION-LIC-05) и сам уведомляет пользователя; LIC TTL — safety net на случай watchdog drift.

---

## 3. RE_CHECK — повторная проверка с дельтой рисков

Соответствует `high-architecture.md` §8.3.

```mermaid
sequenceDiagram
    autonumber
    participant DM as Document Management
    participant LIC as LIC Service
    participant Redis as Redis
    participant LLM as LLM Provider Router
    participant Orch as Orchestrator

    Note over DM: Пользователь запросил RE_CHECK\nDM создаёт новую версию version_id=N+1
    DM->>LIC: dm.events.version-created\n{origin_type:"RE_CHECK",\nparent_version_id:N,\nversion_id:N+1}
    LIC->>Redis: SET lic-version-meta:{N+1}\n{origin_type:RE_CHECK, parent_version_id:N}\n(ttl 24h)
    Note over LIC: ACK
    Note over DM: DP обрабатывает; артефакты сохранены
    DM->>LIC: dm.events.version-artifacts-ready\n(version_id=N+1)
    LIC->>Redis: GET lic-version-meta:{N+1}
    Redis-->>LIC: {origin_type:RE_CHECK, parent_version_id:N}
    par Two artifact requests in parallel
        LIC->>DM: lic.requests.artifacts\n(version_id=N+1, types=base set)
        DM-->>LIC: dm.responses.artifacts-provided\n(target artifacts)
    and
        LIC->>DM: lic.requests.artifacts\n(version_id=N, types=[RISK_ANALYSIS])
        DM-->>LIC: dm.responses.artifacts-provided\n(parent RISK_ANALYSIS)
    end
    Note over LIC: Stage 1 — Stage 5 (как в §1)
    LIC->>LLM: ... 8 agents
    Note over LIC: Stage 6 — Risk Delta
    LIC->>LLM: RiskDelta\n(target.risks, parent.risks)
    LLM-->>LIC: RiskDelta result
    LIC->>LIC: RISK_PROFILE + AGGREGATE_SCORE (deterministic)
    LIC->>DM: lic.artifacts.analysis-ready\n+ risk_delta (extension v1.1)
    DM-->>LIC: dm.responses.lic-artifacts-persisted
    LIC->>Orch: lic.events.status-changed (COMPLETED)
```

---

## 4. Ошибка LLM-провайдера (retryable) → fallback

Соответствует `high-architecture.md` §8.4.

```mermaid
sequenceDiagram
    autonumber
    participant Agent as Agent (RiskDetection)
    participant Router as Provider Router
    participant Claude as ClaudeProvider
    participant OpenAI as OpenAIProvider
    participant Metric as Prometheus

    Agent->>Router: Complete(req)
    Router->>Claude: Complete (1st attempt)
    Claude-->>Router: 503 ServiceUnavailable
    Router->>Metric: lic_llm_provider_failed_total\n{provider:claude,reason:5xx}
    Router->>Claude: Complete (2nd retry, backoff 200ms)
    Claude-->>Router: 503 ServiceUnavailable
    Router->>Metric: lic_llm_provider_failed_total
    Note over Router: Provider Router: Claude marked unhealthy\n(consecutive_failures >= 3)
    Router->>OpenAI: Complete (fallback)
    OpenAI-->>Router: 200 OK + valid JSON
    Router->>Metric: lic_llm_provider_fallback_total\n{from:claude,to:openai}
    Router-->>Agent: CompletionResponse
```

---

## 5. Невалидный JSON → repair loop

Соответствует `high-architecture.md` §8.5.

```mermaid
sequenceDiagram
    autonumber
    participant LIC as LIC Pipeline
    participant Agent as Agent (RiskDetection)
    participant LLM as LLM Provider
    participant Validator as Schema Validator
    participant DLQ as DLQ Publisher
    participant Orch as Orchestrator

    LIC->>Agent: Run(input)
    Agent->>LLM: Complete (1st)
    LLM-->>Agent: Raw response (non-JSON or schema violation)
    Agent->>Validator: Validate(response)
    Validator-->>Agent: ValidationError
    Note over Agent: Repair Loop × 1
    Agent->>LLM: Complete (repair)\n"Твой ответ не прошёл валидацию: <errors>.\nИсправь под исходную схему."
    LLM-->>Agent: New response
    Agent->>Validator: Validate
    alt repair succeeded
        Validator-->>Agent: OK
        Agent-->>LIC: AgentResult
    else repair failed
        Validator-->>Agent: ValidationError again
        Agent-->>LIC: ErrAgentOutputInvalid
        LIC->>Orch: lic.events.status-changed\n{status:FAILED, error_code:AGENT_OUTPUT_INVALID, is_retryable:true,\n error_message:"Не удалось получить корректный анализ. Запустите повторную проверку."}
        LIC->>DLQ: lic.dlq.agent-output-invalid\n{agent_id, raw_response_hash}
        Note over LIC: ACK исходного сообщения
    end
```

---

## 6. Таймаут DM на запросе артефактов

Соответствует `high-architecture.md` §8.6.

```mermaid
sequenceDiagram
    autonumber
    participant LIC as LIC Service
    participant Awaiter as DM Artifact Awaiter
    participant DM as Document Management
    participant Orch as Orchestrator
    participant DLQ as DLQ Publisher

    LIC->>DM: lic.requests.artifacts
    LIC->>Awaiter: Wait(correlation_id, ttl=30s)
    Note over Awaiter: 30 seconds elapse
    Awaiter-->>LIC: ErrDMArtifactsTimeout
    LIC->>Orch: lic.events.status-changed\n{status:FAILED, error_code:DM_ARTIFACTS_TIMEOUT, is_retryable:true,\n error_message:"Не удалось получить данные документа. Попробуйте позже."}
    LIC->>DLQ: lic.dlq.consumer-failed
    Note over LIC: NACK с requeue → next attempt
    Note over LIC,Awaiter: При исчерпании retry — DLQ финал
```

---

## 7. RE_CHECK без родительского RISK_ANALYSIS — graceful degradation

Соответствует `high-architecture.md` §8.7.

```mermaid
sequenceDiagram
    autonumber
    participant LIC as LIC Service
    participant DM as Document Management
    participant LLM as LLM Provider

    Note over LIC: RE_CHECK обнаружен;\nзапрашиваем родительский RISK_ANALYSIS
    LIC->>DM: lic.requests.artifacts\n(version_id=N, types=[RISK_ANALYSIS])
    DM-->>LIC: dm.responses.artifacts-provided\n{artifacts:{}, missing_types:["RISK_ANALYSIS"]}
    Note over LIC: Skip Stage 6 (RiskDelta)\nrisk_delta=null\nadd warning RE_CHECK_PARENT_ANALYSIS_MISSING
    Note over LIC: Stage 1-5 продолжаются\nDetailedReport получает warning
    LIC->>LLM: ... (regular pipeline without RiskDelta)
    LIC->>DM: lic.artifacts.analysis-ready\n(risk_delta absent)
```

---

## 8. DM persist failed (non-retryable)

Соответствует `high-architecture.md` §8.8.

```mermaid
sequenceDiagram
    autonumber
    participant LIC as LIC Service
    participant DM as Document Management
    participant Orch as Orchestrator

    LIC->>DM: lic.artifacts.analysis-ready
    DM-->>LIC: dm.responses.lic-artifacts-persist-failed\n{error_code:DOCUMENT_NOT_FOUND, is_retryable:false}
    LIC->>Orch: lic.events.status-changed\n{status:FAILED, error_code:DM_PERSIST_FAILED, is_retryable:false,\n error_message:"Документ был удалён или недоступен. Анализ невозможно сохранить."}
    Note over LIC: ACK сообщения, audit-лог
```

---

## 9. Повторная доставка одного и того же события

Соответствует `high-architecture.md` §8.9.

```mermaid
sequenceDiagram
    autonumber
    participant DM as Document Management
    participant LIC as LIC Service
    participant Redis as Redis

    DM->>LIC: dm.events.version-artifacts-ready\n(дубликат)
    LIC->>Redis: GET lic-trigger:{version_id}
    Redis-->>LIC: COMPLETED
    Note over LIC: Skip processing (idempotency)
    Note over LIC: ACK дубликата
```

---

## 10. Превышение бюджета времени (timeout pipeline)

Соответствует `high-architecture.md` §8.10.

```mermaid
sequenceDiagram
    autonumber
    participant LIC as LIC Pipeline
    participant LLM as LLM Provider
    participant Orch as Orchestrator
    participant DLQ as DLQ Publisher

    Note over LIC: Pipeline Orchestrator starts with\nctx, _ := context.WithTimeout(ctx, LIC_JOB_TIMEOUT=90s)
    LIC->>LLM: Stage 1 ... Stage 4 (taking long)
    Note over LIC: 90 seconds elapse
    LIC->>LLM: cancel context
    LLM-->>LIC: context.Canceled
    LIC->>Orch: lic.events.status-changed\n{status:FAILED, error_code:ANALYSIS_TIMEOUT, is_retryable:true,\n error_message:"Анализ занял слишком много времени. Запустите повторную проверку."}
    LIC->>DLQ: lic.dlq.consumer-failed\n(если retry exhausted)
    Note over LIC: NACK с requeue
```

---

## 11. End-to-end: загрузка договора → готовый анализ (контекстная диаграмма)

Иллюстрирует место LIC в общем потоке системы (для понимания границ; реализация — у соответствующих доменов).

```mermaid
sequenceDiagram
    autonumber
    actor User
    participant Orch as Orchestrator
    participant DP as Document Processing
    participant DM as Document Management
    participant LIC as LIC Service
    participant RE as Reporting Engine

    User->>Orch: POST /upload (PDF)
    Orch->>DM: POST /documents (sync REST)
    Orch->>DP: dp.commands.process-document
    DP->>DM: dp.artifacts.processing-ready
    DM->>LIC: dm.events.version-artifacts-ready
    Note over LIC: LIC pipeline (см. §1 этого документа)
    LIC->>DM: lic.artifacts.analysis-ready
    DM->>RE: dm.events.version-analysis-ready
    Note over RE: Generates PDF/DOCX
    RE->>DM: re.artifacts.reports-ready
    DM->>Orch: dm.events.version-reports-ready
    Orch->>User: SSE: "Готово"
```

---

## 12. Параллельные стадии в одном инстансе

Иллюстрирует concurrent processing нескольких jobs.

```mermaid
sequenceDiagram
    autonumber
    participant DM as Document Management
    participant Cons as Event Consumer\n(prefetch=10)
    participant Sema as Semaphore\nLIC_PIPELINE_CONCURRENCY=5
    participant Pipe as Pipeline Orchestrator

    DM->>Cons: 7 events (different version_ids)
    par Job 1
        Cons->>Sema: Acquire
        Sema-->>Cons: OK
        Cons->>Pipe: Run(job1)
    and Job 2..5
        Cons->>Sema: Acquire
        Sema-->>Cons: OK
        Cons->>Pipe: Run(...)
    and Jobs 6, 7
        Cons->>Sema: Acquire
        Note over Cons: blocked until\nslot frees
    end
    Note over Pipe: Job 1 finishes
    Pipe-->>Sema: Release
    Note over Cons: Slot frees → Job 6 starts
```

---

## 13. Проверка контрольной суммы ИНН/ОГРН (Pre-LLM step для агента 3)

Иллюстрирует деривативный шаг внутри агента 3 (Party Consistency) — детерминированная проверка контрольных сумм перед LLM-вызовом (для уменьшения галлюцинаций и стоимости).

```mermaid
sequenceDiagram
    autonumber
    participant Agent3 as Party Consistency Agent
    participant Validator as Native INN/OGRN Validator (Go)
    participant Builder as Prompt Builder
    participant LLM as LLM Provider

    Note over Agent3: Получены party_roles из Agent2
    loop for each party
        Agent3->>Validator: ValidateINN(inn) / ValidateOGRN(ogrn)
        Validator-->>Agent3: {valid: bool, entity_type: string|null}
    end
    Agent3->>Builder: BuildPrompt(party_roles, validation_facts, document)
    Builder-->>Agent3: <input> with <validation_facts>
    Agent3->>LLM: Complete
    LLM-->>Agent3: PartyConsistencyFindings\n(LLM использует факты, не валидирует сам)
```

---

## 14. Цикл audit и observability

Иллюстрирует формирование observability-сигналов на ключевых точках пайплайна.

```mermaid
sequenceDiagram
    autonumber
    participant LIC as LIC Pipeline
    participant Log as Structured Log
    participant Prom as Prometheus
    participant OTel as OpenTelemetry Collector

    Note over LIC: Получено dm.events.version-artifacts-ready
    LIC->>Log: INFO {correlation_id, job_id, organization_id, event:received}
    LIC->>OTel: span.start("lic.pipeline")
    LIC->>Prom: lic_pipeline_started_total{}
    Note over LIC: ... выполнение пайплайна ...
    loop per agent invocation
        LIC->>OTel: span.start("lic.agent." + agent_id)
        LIC->>Log: INFO {agent, model, tokens_in, tokens_out, latency, outcome}
        LIC->>Prom: lic_agent_invocations_total + cost + duration
        LIC->>OTel: span.end with attributes
    end
    Note over LIC: Pipeline completed
    LIC->>Prom: lic_pipeline_total_duration_seconds{outcome:success}
    LIC->>OTel: span.end("lic.pipeline")
    LIC->>Log: INFO {outcome:completed, total_cost_usd}
```
