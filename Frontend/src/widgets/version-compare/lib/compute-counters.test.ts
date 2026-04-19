import { describe, expect, it } from 'vitest';

import type { VersionDiffResult } from '@/features/comparison-start';

import { computeChangeCounters } from './compute-counters';

function makeDiff(overrides: Partial<VersionDiffResult> = {}): VersionDiffResult {
  return {
    baseVersionId: 'base',
    targetVersionId: 'target',
    textDiffCount: 0,
    structuralDiffCount: 0,
    textDiffs: [],
    structuralDiffs: [],
    ...overrides,
  };
}

describe('computeChangeCounters', () => {
  it('возвращает нули для пустого diff', () => {
    const result = computeChangeCounters(makeDiff());
    expect(result).toEqual({
      total: 0,
      added: 0,
      removed: 0,
      modified: 0,
      moved: 0,
      textual: 0,
      structural: 0,
    });
  });

  it('считает текстовые added/removed/modified', () => {
    const result = computeChangeCounters(
      makeDiff({
        textDiffCount: 3,
        textDiffs: [
          { type: 'added', path: 'a' },
          { type: 'removed', path: 'b' },
          { type: 'modified', path: 'c' },
        ],
      }),
    );
    expect(result.added).toBe(1);
    expect(result.removed).toBe(1);
    expect(result.modified).toBe(1);
    expect(result.textual).toBe(3);
    expect(result.structural).toBe(0);
    expect(result.total).toBe(3);
  });

  it('считает структурные moved', () => {
    const result = computeChangeCounters(
      makeDiff({
        structuralDiffCount: 2,
        structuralDiffs: [
          { type: 'moved', node_id: 's1' },
          { type: 'moved', node_id: 's2' },
        ],
      }),
    );
    expect(result.moved).toBe(2);
    expect(result.structural).toBe(2);
    expect(result.total).toBe(2);
  });

  it('суммирует текст + структуру', () => {
    const result = computeChangeCounters(
      makeDiff({
        textDiffCount: 2,
        structuralDiffCount: 3,
        textDiffs: [
          { type: 'added', path: 'a' },
          { type: 'modified', path: 'b' },
        ],
        structuralDiffs: [
          { type: 'added', node_id: 's1' },
          { type: 'removed', node_id: 's2' },
          { type: 'moved', node_id: 's3' },
        ],
      }),
    );
    expect(result.added).toBe(2);
    expect(result.removed).toBe(1);
    expect(result.modified).toBe(1);
    expect(result.moved).toBe(1);
    expect(result.textual).toBe(2);
    expect(result.structural).toBe(3);
    expect(result.total).toBe(5);
  });

  it('игнорирует записи с неизвестным/undefined type', () => {
    const result = computeChangeCounters(
      makeDiff({
        textDiffCount: 1,
        textDiffs: [{ path: 'no-type' }],
      }),
    );
    expect(result.added).toBe(0);
    expect(result.removed).toBe(0);
    expect(result.modified).toBe(0);
    expect(result.textual).toBe(1);
  });
});
