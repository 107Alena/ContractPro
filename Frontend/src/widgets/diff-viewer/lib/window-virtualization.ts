// Window-based виртуализация по параграфам (§11.2 high-architecture).
// Pure-функция: только вход (scrollTop, viewportHeight, rowHeight) → выход.
// Никаких side-effects, легко тестируется и переиспользуется.
//
// rowHeight предполагается фиксированной (договоры обычно 60-200 параграфов,
// варьируем только высоту строки между режимами side-by-side и inline на
// уровне родителя). Variable-height-virtualization сознательно out-of-scope
// для v1.

export interface VisibleWindow {
  /** Индекс первого видимого элемента (включительно). */
  start: number;
  /** Индекс последнего видимого элемента (исключительно, slice-style). */
  end: number;
  /** Смещение в px для translateY-обёртки видимого slice. */
  offsetY: number;
}

/**
 * Возвращает индексный диапазон элементов, которые надо отрендерить, плюс
 * вертикальное смещение для transform.
 *
 * Защитные инварианты:
 * - items.length === 0  → start=0, end=0, offsetY=0
 * - rowHeight <= 0      → деградация в "рендерим всё", чтобы не делить на ноль
 * - scrollTop отрицательный (rubber-band на iOS) → start=0
 * - end не выходит за items.length
 */
export function getVisibleWindow<T>(
  items: readonly T[],
  scrollTop: number,
  viewportHeight: number,
  rowHeight: number,
  overscan: number = 5,
): VisibleWindow {
  const total = items.length;
  if (total === 0) {
    return { start: 0, end: 0, offsetY: 0 };
  }
  if (rowHeight <= 0) {
    return { start: 0, end: total, offsetY: 0 };
  }

  const safeScrollTop = Math.max(0, scrollTop);
  const safeViewport = Math.max(0, viewportHeight);
  const safeOverscan = Math.max(0, Math.floor(overscan));

  const firstVisible = Math.floor(safeScrollTop / rowHeight);
  const lastVisible = Math.ceil((safeScrollTop + safeViewport) / rowHeight);

  const start = Math.max(0, firstVisible - safeOverscan);
  const end = Math.min(total, lastVisible + safeOverscan);

  return {
    start,
    end,
    offsetY: start * rowHeight,
  };
}
