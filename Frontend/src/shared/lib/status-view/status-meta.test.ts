import { describe, expect, it } from 'vitest';

import type { UserProcessingStatus } from '@/shared/api';

import { STATUS_META, statusMeta, UNKNOWN_STATUS_META } from './status-meta';

const ALL_STATUSES: readonly UserProcessingStatus[] = [
  'UPLOADED',
  'QUEUED',
  'PROCESSING',
  'ANALYZING',
  'AWAITING_USER_INPUT',
  'GENERATING_REPORTS',
  'READY',
  'PARTIALLY_FAILED',
  'FAILED',
  'REJECTED',
];

describe('STATUS_META', () => {
  it('covers all 10 UserProcessingStatus values from §5.2', () => {
    for (const status of ALL_STATUSES) {
      expect(STATUS_META).toHaveProperty(status);
      expect(STATUS_META[status].label).toBeTruthy();
      expect(STATUS_META[status].tone).toBeTruthy();
    }
  });

  it('maps terminal success to success tone', () => {
    expect(STATUS_META.READY.tone).toBe('success');
  });

  it('maps terminal failures to danger tone', () => {
    expect(STATUS_META.FAILED.tone).toBe('danger');
    expect(STATUS_META.REJECTED.tone).toBe('danger');
  });

  it('maps user-input-required and partial-failure to warning tone', () => {
    expect(STATUS_META.AWAITING_USER_INPUT.tone).toBe('warning');
    expect(STATUS_META.PARTIALLY_FAILED.tone).toBe('warning');
  });

  it('maps active processing phases to brand tone', () => {
    expect(STATUS_META.PROCESSING.tone).toBe('brand');
    expect(STATUS_META.ANALYZING.tone).toBe('brand');
    expect(STATUS_META.GENERATING_REPORTS.tone).toBe('brand');
  });

  it('maps pending/queued to neutral tone', () => {
    expect(STATUS_META.UPLOADED.tone).toBe('neutral');
    expect(STATUS_META.QUEUED.tone).toBe('neutral');
  });

  it('labels are non-empty Russian strings (NFR-5.2)', () => {
    for (const status of ALL_STATUSES) {
      expect(STATUS_META[status].label.length).toBeGreaterThan(0);
      expect(STATUS_META[status].label).toMatch(/[а-яё]/i);
    }
  });
});

describe('statusMeta()', () => {
  it('returns mapped meta for a known status', () => {
    expect(statusMeta('READY')).toEqual({ label: 'Готово', tone: 'success' });
  });

  it('returns UNKNOWN_STATUS_META for undefined', () => {
    expect(statusMeta(undefined)).toBe(UNKNOWN_STATUS_META);
  });

  it('returns UNKNOWN_STATUS_META for null', () => {
    expect(statusMeta(null)).toBe(UNKNOWN_STATUS_META);
  });
});
