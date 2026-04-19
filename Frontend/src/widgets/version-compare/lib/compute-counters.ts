// Агрегатор VersionDiffResult → ChangeCountersValue.
//
// Контракт VersionDiffResult (см. features/comparison-start/model/types):
//   - text_diffs[].type  ∈ 'added' | 'removed' | 'modified'
//   - structural_diffs[].type ∈ 'added' | 'removed' | 'modified' | 'moved'
//   - text_diff_count / structural_diff_count — серверные суммы (НЕ тот же
//     набор, что .length массивов: API может возвращать саммари без полного
//     списка, см. VersionDiff в OpenAPI). Для total используем серверные
//     суммы, для added/removed/modified/moved/textual/structural —
//     фактические массивы.
import type { VersionDiffResult } from '@/features/comparison-start';

import type { ChangeCountersValue } from '../model/types';

export function computeChangeCounters(diff: VersionDiffResult): ChangeCountersValue {
  let added = 0;
  let removed = 0;
  let modified = 0;
  let moved = 0;

  for (const change of diff.textDiffs) {
    if (change.type === 'added') added += 1;
    else if (change.type === 'removed') removed += 1;
    else if (change.type === 'modified') modified += 1;
  }

  for (const change of diff.structuralDiffs) {
    if (change.type === 'added') added += 1;
    else if (change.type === 'removed') removed += 1;
    else if (change.type === 'modified') modified += 1;
    else if (change.type === 'moved') moved += 1;
  }

  return {
    total: diff.textDiffCount + diff.structuralDiffCount,
    added,
    removed,
    modified,
    moved,
    textual: diff.textDiffs.length,
    structural: diff.structuralDiffs.length,
  };
}
