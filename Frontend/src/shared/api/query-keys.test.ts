import { describe, expect, expectTypeOf, it } from 'vitest';

import { qk } from './query-keys';
import type { AuditFilters, ListParams } from './types';

describe('qk (query-keys)', () => {
  describe('me', () => {
    it('is readonly ["me"] tuple', () => {
      expect(qk.me).toEqual(['me']);
      expectTypeOf(qk.me).toEqualTypeOf<readonly ['me']>();
    });
  });

  describe('contracts', () => {
    it('all is readonly ["contracts"]', () => {
      expect(qk.contracts.all).toEqual(['contracts']);
      expectTypeOf(qk.contracts.all).toEqualTypeOf<readonly ['contracts']>();
    });

    it('list embeds params tuple', () => {
      const params: ListParams = { page: 2, size: 20, status: 'ACTIVE', search: 'foo' };
      expect(qk.contracts.list(params)).toEqual(['contracts', 'list', params]);
      expectTypeOf(qk.contracts.list).returns.toEqualTypeOf<
        readonly ['contracts', 'list', ListParams]
      >();
    });

    it('byId returns readonly ["contracts", string]', () => {
      expect(qk.contracts.byId('c-1')).toEqual(['contracts', 'c-1']);
      expectTypeOf(qk.contracts.byId).returns.toEqualTypeOf<readonly ['contracts', string]>();
    });

    it('versions returns readonly ["contracts", string, "versions"]', () => {
      expect(qk.contracts.versions('c-1')).toEqual(['contracts', 'c-1', 'versions']);
      expectTypeOf(qk.contracts.versions).returns.toEqualTypeOf<
        readonly ['contracts', string, 'versions']
      >();
    });

    it('version includes version id', () => {
      expect(qk.contracts.version('c-1', 'v-2')).toEqual(['contracts', 'c-1', 'versions', 'v-2']);
    });

    it('status/results/risks/summary/recommendations share prefix', () => {
      expect(qk.contracts.status('c-1', 'v-2')).toEqual([
        'contracts',
        'c-1',
        'versions',
        'v-2',
        'status',
      ]);
      expect(qk.contracts.results('c-1', 'v-2')).toEqual([
        'contracts',
        'c-1',
        'versions',
        'v-2',
        'results',
      ]);
      expect(qk.contracts.risks('c-1', 'v-2')).toEqual([
        'contracts',
        'c-1',
        'versions',
        'v-2',
        'risks',
      ]);
      expect(qk.contracts.summary('c-1', 'v-2')).toEqual([
        'contracts',
        'c-1',
        'versions',
        'v-2',
        'summary',
      ]);
      expect(qk.contracts.recommendations('c-1', 'v-2')).toEqual([
        'contracts',
        'c-1',
        'versions',
        'v-2',
        'recommendations',
      ]);
    });

    it('diff embeds both version ids', () => {
      expect(qk.contracts.diff('c-1', 'v-2', 'v-3')).toEqual([
        'contracts',
        'c-1',
        'diff',
        'v-2',
        'v-3',
      ]);
    });
  });

  describe('admin', () => {
    it('policies and checklists are readonly tuples', () => {
      expect(qk.admin.policies).toEqual(['admin', 'policies']);
      expect(qk.admin.checklists).toEqual(['admin', 'checklists']);
      expectTypeOf(qk.admin.policies).toEqualTypeOf<readonly ['admin', 'policies']>();
      expectTypeOf(qk.admin.checklists).toEqualTypeOf<readonly ['admin', 'checklists']>();
    });
  });

  describe('audit', () => {
    it('embeds filters object', () => {
      const filters: AuditFilters = { from: '2026-01-01', user_id: 'u-1' };
      expect(qk.audit(filters)).toEqual(['audit', filters]);
      expectTypeOf(qk.audit).returns.toEqualTypeOf<readonly ['audit', AuditFilters]>();
    });
  });

  describe('hierarchy for invalidation', () => {
    it('all contract queries share ["contracts"] prefix', () => {
      const keys: readonly unknown[][] = [
        [...qk.contracts.all],
        [...qk.contracts.byId('c-1')],
        [...qk.contracts.versions('c-1')],
        [...qk.contracts.version('c-1', 'v-2')],
        [...qk.contracts.status('c-1', 'v-2')],
        [...qk.contracts.results('c-1', 'v-2')],
        [...qk.contracts.risks('c-1', 'v-2')],
        [...qk.contracts.summary('c-1', 'v-2')],
        [...qk.contracts.recommendations('c-1', 'v-2')],
        [...qk.contracts.diff('c-1', 'v-2', 'v-3')],
        [...qk.contracts.list({})],
      ];
      for (const key of keys) {
        expect(key[0]).toBe('contracts');
      }
    });
  });
});
