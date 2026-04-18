// Pure-функции can() и canExport() — тесты в node-environment (default).
// Хук-тесты — см. rbac.hooks.test.tsx (docblock jsdom).
import { describe, expect, it } from 'vitest';

import { can, PERMISSIONS } from './rbac';
import { canExport } from './use-can-export';

describe('PERMISSIONS table (§5.5)', () => {
  it('1:1 с архитектурной таблицей', () => {
    expect(PERMISSIONS).toStrictEqual({
      'contract.upload': ['LAWYER', 'BUSINESS_USER', 'ORG_ADMIN'],
      'contract.archive': ['LAWYER', 'ORG_ADMIN'],
      'risks.view': ['LAWYER', 'ORG_ADMIN'],
      'summary.view': ['LAWYER', 'BUSINESS_USER', 'ORG_ADMIN'],
      'recommendations.view': ['LAWYER', 'ORG_ADMIN'],
      'comparison.run': ['LAWYER', 'ORG_ADMIN'],
      'version.recheck': ['LAWYER', 'ORG_ADMIN'],
      'version.confirm-type': ['LAWYER', 'ORG_ADMIN'],
      'admin.policies': ['ORG_ADMIN'],
      'admin.checklists': ['ORG_ADMIN'],
      'audit.view': ['ORG_ADMIN'],
      'export.download': ['LAWYER', 'ORG_ADMIN'],
    });
  });
});

describe('can() pure', () => {
  it('undefined role → всегда false', () => {
    expect(can(undefined, 'contract.upload')).toBe(false);
    expect(can(undefined, 'admin.policies')).toBe(false);
  });

  it('LAWYER: читает риски/рекомендации/загрузка, но не admin/audit', () => {
    expect(can('LAWYER', 'risks.view')).toBe(true);
    expect(can('LAWYER', 'recommendations.view')).toBe(true);
    expect(can('LAWYER', 'contract.upload')).toBe(true);
    expect(can('LAWYER', 'comparison.run')).toBe(true);
    expect(can('LAWYER', 'export.download')).toBe(true);
    expect(can('LAWYER', 'admin.policies')).toBe(false);
    expect(can('LAWYER', 'admin.checklists')).toBe(false);
    expect(can('LAWYER', 'audit.view')).toBe(false);
  });

  it('BUSINESS_USER: только upload + summary (R-2 ТЗ)', () => {
    expect(can('BUSINESS_USER', 'contract.upload')).toBe(true);
    expect(can('BUSINESS_USER', 'summary.view')).toBe(true);
    expect(can('BUSINESS_USER', 'risks.view')).toBe(false);
    expect(can('BUSINESS_USER', 'recommendations.view')).toBe(false);
    expect(can('BUSINESS_USER', 'contract.archive')).toBe(false);
    expect(can('BUSINESS_USER', 'comparison.run')).toBe(false);
    expect(can('BUSINESS_USER', 'version.recheck')).toBe(false);
    expect(can('BUSINESS_USER', 'export.download')).toBe(false);
    expect(can('BUSINESS_USER', 'admin.policies')).toBe(false);
    expect(can('BUSINESS_USER', 'audit.view')).toBe(false);
  });

  it('ORG_ADMIN: всё, включая admin.* и audit.view', () => {
    expect(can('ORG_ADMIN', 'contract.upload')).toBe(true);
    expect(can('ORG_ADMIN', 'risks.view')).toBe(true);
    expect(can('ORG_ADMIN', 'admin.policies')).toBe(true);
    expect(can('ORG_ADMIN', 'admin.checklists')).toBe(true);
    expect(can('ORG_ADMIN', 'audit.view')).toBe(true);
    expect(can('ORG_ADMIN', 'export.download')).toBe(true);
    expect(can('ORG_ADMIN', 'comparison.run')).toBe(true);
    expect(can('ORG_ADMIN', 'version.recheck')).toBe(true);
  });
});

describe('canExport() pure (§5.6)', () => {
  it('LAWYER — true независимо от export_enabled', () => {
    expect(canExport('LAWYER', true)).toBe(true);
    expect(canExport('LAWYER', false)).toBe(true);
    expect(canExport('LAWYER', undefined)).toBe(true);
  });

  it('ORG_ADMIN — true независимо от export_enabled', () => {
    expect(canExport('ORG_ADMIN', true)).toBe(true);
    expect(canExport('ORG_ADMIN', false)).toBe(true);
    expect(canExport('ORG_ADMIN', undefined)).toBe(true);
  });

  it('BUSINESS_USER — зависит от permissions.export_enabled', () => {
    expect(canExport('BUSINESS_USER', true)).toBe(true);
    expect(canExport('BUSINESS_USER', false)).toBe(false);
    // default-fallback от backend (ORCH_OPM_FALLBACK_BUSINESS_USER_EXPORT=false):
    // undefined должен считаться отсутствием разрешения.
    expect(canExport('BUSINESS_USER', undefined)).toBe(false);
  });

  it('Отсутствие роли (не-авторизованный) — false', () => {
    expect(canExport(undefined, true)).toBe(false);
    expect(canExport(undefined, false)).toBe(false);
    expect(canExport(undefined, undefined)).toBe(false);
  });
});
