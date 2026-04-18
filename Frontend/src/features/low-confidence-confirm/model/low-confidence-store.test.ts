import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { useLowConfidenceStore } from './low-confidence-store';
import type { TypeConfirmationEvent } from './types';

function makeEvent(overrides: Partial<TypeConfirmationEvent> = {}): TypeConfirmationEvent {
  return {
    document_id: 'doc-1',
    version_id: 'ver-1',
    status: 'AWAITING_USER_INPUT',
    suggested_type: 'услуги',
    confidence: 0.62,
    threshold: 0.75,
    alternatives: [
      { contract_type: 'подряд', confidence: 0.21 },
      { contract_type: 'NDA', confidence: 0.1 },
    ],
    ...overrides,
  };
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-04-18T10:00:00Z'));
  useLowConfidenceStore.getState().__reset();
});

afterEach(() => {
  vi.useRealTimers();
  useLowConfidenceStore.getState().__reset();
});

describe('low-confidence-store — open / dismiss / resolve', () => {
  it('open → current = event', () => {
    const e = makeEvent();
    useLowConfidenceStore.getState().open(e);
    expect(useLowConfidenceStore.getState().current).toBe(e);
  });

  it('dismiss → current=null + версия попадает в recent', () => {
    useLowConfidenceStore.getState().open(makeEvent());
    useLowConfidenceStore.getState().dismiss();
    const s = useLowConfidenceStore.getState();
    expect(s.current).toBeNull();
    expect(s.recent.map((r) => r.versionId)).toEqual(['ver-1']);
  });

  it('resolve → current=null + версия в recent (как dismiss)', () => {
    useLowConfidenceStore.getState().open(makeEvent());
    useLowConfidenceStore.getState().resolve();
    const s = useLowConfidenceStore.getState();
    expect(s.current).toBeNull();
    expect(s.recent.map((r) => r.versionId)).toEqual(['ver-1']);
  });

  it('dismiss/resolve без активного event → no-op', () => {
    useLowConfidenceStore.getState().dismiss();
    useLowConfidenceStore.getState().resolve();
    expect(useLowConfidenceStore.getState().recent).toEqual([]);
  });
});

describe('low-confidence-store — idempotency (recent LRU)', () => {
  it('повторный open для УЖЕ открытой версии — no-op (защита от SSE retry)', () => {
    const first = makeEvent({ confidence: 0.6 });
    const dup = makeEvent({ confidence: 0.61 });
    useLowConfidenceStore.getState().open(first);
    useLowConfidenceStore.getState().open(dup);
    expect(useLowConfidenceStore.getState().current).toBe(first);
  });

  it('open для версии в recent (после dismiss) — игнорируется в течение TTL=60s', () => {
    useLowConfidenceStore.getState().open(makeEvent());
    useLowConfidenceStore.getState().dismiss();
    // Через 30s (внутри TTL) повторный event для той же версии — игнор.
    vi.advanceTimersByTime(30_000);
    useLowConfidenceStore.getState().open(makeEvent());
    expect(useLowConfidenceStore.getState().current).toBeNull();
  });

  it('после истечения TTL recent — version_id может быть открыт снова', () => {
    useLowConfidenceStore.getState().open(makeEvent());
    useLowConfidenceStore.getState().dismiss();
    vi.advanceTimersByTime(60_001);
    const reopened = makeEvent();
    useLowConfidenceStore.getState().open(reopened);
    expect(useLowConfidenceStore.getState().current).toBe(reopened);
  });

  it('open для ДРУГОЙ версии при активном event — заменяет current, старая версия в recent', () => {
    const a = makeEvent({ version_id: 'ver-A' });
    const b = makeEvent({ version_id: 'ver-B' });
    useLowConfidenceStore.getState().open(a);
    useLowConfidenceStore.getState().open(b);
    const s = useLowConfidenceStore.getState();
    expect(s.current).toBe(b);
    expect(s.recent.map((r) => r.versionId)).toEqual(['ver-A']);
  });

  it('LRU обрезает recent до 10 элементов', () => {
    for (let i = 0; i < 15; i += 1) {
      useLowConfidenceStore.getState().open(makeEvent({ version_id: `ver-${i}` }));
      useLowConfidenceStore.getState().dismiss();
    }
    const s = useLowConfidenceStore.getState();
    expect(s.recent.length).toBe(10);
    expect(s.recent[0]!.versionId).toBe('ver-5');
    expect(s.recent[9]!.versionId).toBe('ver-14');
  });
});
