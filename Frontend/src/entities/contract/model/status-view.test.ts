import { describe, expect, it } from 'vitest';

import { viewStatus } from './status-view';

describe('viewStatus', () => {
  it('maps READY to success / ready bucket', () => {
    const v = viewStatus('READY');
    expect(v.tone).toBe('success');
    expect(v.bucket).toBe('ready');
    expect(v.label).toBe('Готово');
  });

  it('maps AWAITING_USER_INPUT to warning / awaiting bucket', () => {
    const v = viewStatus('AWAITING_USER_INPUT');
    expect(v.tone).toBe('warning');
    expect(v.bucket).toBe('awaiting');
  });

  it.each(['FAILED', 'REJECTED'] as const)('maps %s to danger / failed bucket', (status) => {
    const v = viewStatus(status);
    expect(v.tone).toBe('danger');
    expect(v.bucket).toBe('failed');
  });

  it.each(['PROCESSING', 'ANALYZING', 'GENERATING_REPORTS'] as const)(
    'maps %s to brand / in_progress bucket',
    (status) => {
      const v = viewStatus(status);
      expect(v.tone).toBe('brand');
      expect(v.bucket).toBe('in_progress');
    },
  );

  it.each(['UPLOADED', 'QUEUED'] as const)('maps %s to neutral / pending bucket', (status) => {
    const v = viewStatus(status);
    expect(v.tone).toBe('neutral');
    expect(v.bucket).toBe('pending');
  });

  it('falls back to neutral/pending when status is undefined', () => {
    const v = viewStatus(undefined);
    expect(v.tone).toBe('neutral');
    expect(v.bucket).toBe('pending');
    expect(v.label).toBe('Неизвестно');
  });

  it('maps PARTIALLY_FAILED to warning / failed bucket', () => {
    const v = viewStatus('PARTIALLY_FAILED');
    expect(v.tone).toBe('warning');
    expect(v.bucket).toBe('failed');
  });
});
