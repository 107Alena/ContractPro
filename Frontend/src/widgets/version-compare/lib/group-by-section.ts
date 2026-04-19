// Группировка изменений по разделам договора → топ-5 секций.
//
// Источники путей:
//   - text_diffs[].path        — например: "section.2/clause.4/text"
//   - structural_diffs[].node_id — например: "section.3.1" или "preamble"
//
// «Раздел» вытаскивается как первый стабильный сегмент:
//   - для path: подстрока до второго '/'  → "section.2/clause.4"
//     слишком детально; берём только первый сегмент → "section.2".
//   - для node_id: берём префикс до первого '.' включая первый числовой
//     индекс ("section.3" вместо "section.3.1") — так агрегация по
//     разделам совпадает между текстовыми и структурными изменениями.
//
// Если нет ни path, ни node_id — относим к секции '—' (без раздела).
import type { VersionDiffResult } from '@/features/comparison-start';

import type { SectionDiffSummary } from '../model/types';

const TOP_N = 5;
const NO_SECTION = '—';

function pathToSection(path: string | undefined): string {
  if (!path) return NO_SECTION;
  const firstSeg = path.split('/')[0];
  return firstSeg && firstSeg.length > 0 ? firstSeg : NO_SECTION;
}

function nodeIdToSection(nodeId: string | undefined): string {
  if (!nodeId) return NO_SECTION;
  const parts = nodeId.split('.');
  if (parts.length <= 2) return nodeId;
  // section.3.1.2 → section.3 (parts[0..2] существуют, length > 2 уже проверено)
  const head = parts[0] ?? '';
  const second = parts[1] ?? '';
  return `${head}.${second}`;
}

interface MutableSection {
  section: string;
  added: number;
  removed: number;
  modified: number;
}

function bump(
  acc: Map<string, MutableSection>,
  key: string,
  type: 'added' | 'removed' | 'modified' | 'moved' | undefined,
): void {
  let entry = acc.get(key);
  if (!entry) {
    entry = { section: key, added: 0, removed: 0, modified: 0 };
    acc.set(key, entry);
  }
  if (type === 'added') entry.added += 1;
  else if (type === 'removed') entry.removed += 1;
  else if (type === 'modified' || type === 'moved') entry.modified += 1;
}

function totalChanges(s: SectionDiffSummary): number {
  return s.added + s.removed + s.modified;
}

export function groupBySection(diff: VersionDiffResult): SectionDiffSummary[] {
  const acc = new Map<string, MutableSection>();

  for (const change of diff.textDiffs) {
    bump(acc, pathToSection(change.path), change.type);
  }
  for (const change of diff.structuralDiffs) {
    bump(acc, nodeIdToSection(change.node_id), change.type);
  }

  const list: SectionDiffSummary[] = Array.from(acc.values()).map((s) => ({
    section: s.section,
    added: s.added,
    removed: s.removed,
    modified: s.modified,
  }));

  // Сортировка: по убыванию суммарных изменений, при равенстве — алфавитно.
  list.sort((a, b) => {
    const diffTotal = totalChanges(b) - totalChanges(a);
    if (diffTotal !== 0) return diffTotal;
    return a.section.localeCompare(b.section, 'ru');
  });

  return list.slice(0, TOP_N);
}
