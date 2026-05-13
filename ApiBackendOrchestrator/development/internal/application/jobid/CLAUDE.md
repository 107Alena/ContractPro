# jobid

Helper для генерации идентификаторов processing-flow.

`NewJobID()` возвращает свежий UUID v4. Сгенерированное значение используется
как стабильный correlation-key, который протекает через всю цепочку
обработки документа:

- DM REST: `CreateVersionRequest.job_id` (persistance в `document_versions.job_id`,
  DM-TASK-054)
- DP RabbitMQ: `ProcessDocumentRequested.job_id`
- LIC и downstream-публикации (артефакты, события состояния)

**Инвариант (ASSUMPTION-ORCH-XX, фиксируется в ORCH-TASK-053):** Orchestrator
генерирует `job_id` ДО REST-вызова DM `POST /documents/{id}/versions`.
То же значение пробрасывается в последующий
`ProcessDocumentRequested` для DP. `job_id` immutable в течение всего
processing-flow данной версии.

Не использовать `uuid.NewString()` прямо в application/handler-коде —
проходить через `jobid.NewJobID()`, чтобы единая точка контроля сохранилась
для будущих изменений (например, добавление префикса, validate-хелперов или
смены реализации).
