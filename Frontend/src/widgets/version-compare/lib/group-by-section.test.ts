import { describe, expect, it } from 'vitest';

import type { VersionDiffResult } from '@/features/comparison-start';

import { groupBySection } from './group-by-section';

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

describe('groupBySection', () => {
  it('агрегирует path и node_id в одну секцию', () => {
    const result = groupBySection(
      makeDiff({
        textDiffs: [
          { type: 'added', path: 'section.2/clause.1' },
          { type: 'modified', path: 'section.2/clause.4' },
        ],
        structuralDiffs: [{ type: 'moved', node_id: 'section.2.3' }],
      }),
    );
    expect(result).toHaveLength(1);
    expect(result[0]?.section).toBe('section.2');
    expect(result[0]?.added).toBe(1);
    expect(result[0]?.modified).toBe(2); // modified + moved (по контракту moved → modified)
  });

  it('сортирует секции по убыванию суммарных изменений и берёт топ-5', () => {
    const textDiffs = Array.from({ length: 10 }, (_, idx) => ({
      type: 'modified' as const,
      // section.0 .. section.9 → у каждой по одному изменению
      path: `section.${idx}/c`,
    }));
    // Накачиваем section.3 — должна стать первой.
    textDiffs.push(
      { type: 'modified', path: 'section.3/c2' },
      { type: 'modified', path: 'section.3/c3' },
      { type: 'modified', path: 'section.3/c4' },
    );
    const result = groupBySection(makeDiff({ textDiffs }));
    expect(result).toHaveLength(5);
    expect(result[0]?.section).toBe('section.3');
    expect(result[0]?.modified).toBe(4);
  });

  it('кладёт записи без path/node_id в секцию "—"', () => {
    const result = groupBySection(
      makeDiff({
        textDiffs: [{ type: 'added' }],
        structuralDiffs: [{ type: 'removed' }],
      }),
    );
    expect(result).toHaveLength(1);
    expect(result[0]?.section).toBe('—');
    expect(result[0]?.added).toBe(1);
    expect(result[0]?.removed).toBe(1);
  });
});
