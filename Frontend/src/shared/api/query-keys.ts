import type { AuditFilters, ListParams } from './types';

export const qk = {
  me: ['me'] as const,
  contracts: {
    all: ['contracts'] as const,
    list: (p: ListParams) => ['contracts', 'list', p] as const,
    byId: (id: string) => ['contracts', id] as const,
    versions: (id: string) => ['contracts', id, 'versions'] as const,
    version: (id: string, vid: string) => ['contracts', id, 'versions', vid] as const,
    status: (id: string, vid: string) => ['contracts', id, 'versions', vid, 'status'] as const,
    results: (id: string, vid: string) => ['contracts', id, 'versions', vid, 'results'] as const,
    risks: (id: string, vid: string) => ['contracts', id, 'versions', vid, 'risks'] as const,
    summary: (id: string, vid: string) => ['contracts', id, 'versions', vid, 'summary'] as const,
    recommendations: (id: string, vid: string) =>
      ['contracts', id, 'versions', vid, 'recommendations'] as const,
    diff: (id: string, b: string, t: string) => ['contracts', id, 'diff', b, t] as const,
  },
  admin: {
    policies: ['admin', 'policies'] as const,
    checklists: ['admin', 'checklists'] as const,
  },
  audit: (f: AuditFilters) => ['audit', f] as const,
} as const;
