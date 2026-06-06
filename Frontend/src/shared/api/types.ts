import type { components, operations } from './openapi.d';

export type DocumentStatus = components['schemas']['DocumentStatus'];

// Тип договора (английский LIC enum, 12 значений ASSUMPTION-LIC-16). RU-лейблы
// для отображения — entities/contract/model/contract-type.ts.
export type ContractType = components['schemas']['ContractType'];

// ListParams — query-параметры GET /contracts. Производный тип из сгенерированного
// OpenAPI (operations.listContracts), чтобы набор параметров всегда совпадал со
// спекой (правило «никаких ручных API-типов», §15.2). `NonNullable` снимает
// `| undefined` с `query?`. Все поля опциональны; массивы (contract_type /
// processing_status) — mutable, как в generated-типе. Покрывает page/size/status/
// search + risk_level (single) / contract_type[] / processing_status[] /
// date_from / date_to / sort / order (ORCH-TASK-056).
export type ListParams = NonNullable<operations['listContracts']['parameters']['query']>;

// Placeholder: /audit OpenAPI schema not yet defined in Orchestrator spec.
// Заменить точным типом, когда появится AuditQueryParams в openapi.d.ts.
export type AuditFilters = Readonly<Record<string, unknown>>;
