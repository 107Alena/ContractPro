// Типы виджета DiffViewer (§11.2 high-architecture).
// Изолированы в model/, чтобы UI/lib/worker могли импортировать только types
// без захвата diff-match-patch в свой граф (главный chunk должен оставаться
// маленьким — diff-match-patch уезжает в `chunks/diff-viewer` через worker).

/** Режим отображения diff. */
export type DiffMode = 'side-by-side' | 'inline';

/**
 * Параграф, для которого считается diff. id обязателен и стабилен — это
 * react-key и якорь для виртуализации (см. lib/window-virtualization).
 *
 * status === 'unchanged' — fast-path: lib/compute-diff не вызывает
 * diff-match-patch и сразу возвращает один equal-сегмент.
 */
export interface DiffParagraph {
  id: string;
  baseText: string;
  targetText: string;
  status: 'added' | 'removed' | 'modified' | 'unchanged';
  section?: string;
}

/** Один сегмент внутри одного параграфа после diff_main + diff_cleanupSemantic. */
export interface DiffSegment {
  kind: 'equal' | 'insert' | 'delete';
  text: string;
}

/** Результат вычисления diff для одного параграфа. */
export interface ComputedDiffParagraph {
  paragraph: DiffParagraph;
  segments: DiffSegment[];
}
