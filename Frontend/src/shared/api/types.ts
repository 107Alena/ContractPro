import type { components } from './openapi.d';

export type DocumentStatus = components['schemas']['DocumentStatus'];

export interface ListParams {
  page?: number;
  size?: number;
  status?: DocumentStatus;
  search?: string;
}

// Placeholder: /audit OpenAPI schema not yet defined in Orchestrator spec.
// Заменить точным типом, когда появится AuditQueryParams в openapi.d.ts.
export type AuditFilters = Readonly<Record<string, unknown>>;
