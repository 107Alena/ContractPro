# Document Processing Architecture — CLAUDE.md

Russian-language design documentation for the Document Processing domain.

## Files

**high-architecture.md** — Detailed DP domain architecture (v2):
- Entity model (ProcessingJob, ComparisonJob, InputDocumentReference, ExtractedText, DocumentStructure, SemanticTree, Diff)
- Component diagram (validator, OCR adapter, text extractor, structure extractor, semantic tree builder, comparer)
- Data flows for processing and comparison pipelines
- Pipeline stages and their outputs
- Error handling and domain constraints

**configuration.md** — Full environment variable reference:
- All `DP_*` prefixed vars for DP service
- Broker config (address, exchange, queues)
- Storage (endpoint, bucket, credentials)
- OCR service (endpoint, API key, folder ID, RPS limits)
- KVStore (Redis)
- Timeouts, concurrency, file size/page limits
- Observability (tracing, logging, DLQ)

**deployment.md** — (Coming soon) Local development and production deployment using Docker Compose.

All documentation is in Russian. See `/development/` for Go source code.
