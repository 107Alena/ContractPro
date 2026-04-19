// DiffViewer — главный компонент виджета (§11.2 / §17.4 high-architecture).
//
// Архитектура:
//  - Diff считается в Web Worker (см. lib/use-diff-worker). Главный поток
//    остаётся отзывчивым даже на больших договорах (~100 параграфов).
//  - Window-based виртуализация по параграфам (lib/window-virtualization):
//    рендерим только видимый slice + overscan; остальное — height-spacer.
//  - Mode (side-by-side / inline) — uncontrolled fallback с возможностью
//    controlled-overrides через mode/onModeChange.
//
// Performance/bundle:
//  - diff-match-patch не импортируется UI-слоем напрямую. Только worker.ts
//    и lib/compute-diff.ts его знают. Vite кладёт всю папку widgets/diff-viewer
//    + diff-match-patch в chunks/diff-viewer (vite.config.ts).
import { type UIEvent, useCallback, useMemo, useRef, useState } from 'react';

import { cn } from '@/shared/lib/cn';
import { Button } from '@/shared/ui/button';
import { Spinner } from '@/shared/ui/spinner';

import { useDiffWorker } from '../lib/use-diff-worker';
import { getVisibleWindow } from '../lib/window-virtualization';
import type { ComputedDiffParagraph, DiffMode, DiffParagraph } from '../model/types';
import { DiffRow } from './diff-row';
import { DiffToolbar } from './diff-toolbar';

const DEFAULT_ROW_HEIGHT = 64;
const DEFAULT_OVERSCAN = 5;
const DEFAULT_VIEWPORT_HEIGHT = 480;

export interface DiffViewerProps {
  paragraphs?: readonly DiffParagraph[];
  isLoading?: boolean;
  /** unknown — соответствует TanStack Query error и rejected SSE payload. */
  error?: unknown;
  mode?: DiffMode;
  onModeChange?: (mode: DiffMode) => void;
  rowHeight?: number;
  overscan?: number;
  viewportHeight?: number;
  className?: string;
  /** Опциональный retry-callback для error-state. */
  onRetry?: () => void;
}

function countChanges(computed: readonly ComputedDiffParagraph[]): number {
  let n = 0;
  for (const c of computed) {
    if (c.paragraph.status !== 'unchanged') n += 1;
  }
  return n;
}

export function DiffViewer({
  paragraphs,
  isLoading = false,
  error,
  mode: modeProp,
  onModeChange,
  rowHeight = DEFAULT_ROW_HEIGHT,
  overscan = DEFAULT_OVERSCAN,
  viewportHeight = DEFAULT_VIEWPORT_HEIGHT,
  className,
  onRetry,
}: DiffViewerProps): JSX.Element {
  // Uncontrolled fallback: если mode не передан, держим внутреннее состояние.
  const [internalMode, setInternalMode] = useState<DiffMode>('side-by-side');
  const mode = modeProp ?? internalMode;
  const handleModeChange = useCallback(
    (next: DiffMode) => {
      if (modeProp === undefined) setInternalMode(next);
      onModeChange?.(next);
    },
    [modeProp, onModeChange],
  );

  // Loading-state из props (например, ждём ответа от /api/v1/comparisons/{id})
  // имеет приоритет — diff не считаем, пока сами параграфы не пришли.
  const workerState = useDiffWorker(paragraphs);
  const computingDiff = workerState.isComputing;
  const computeError = workerState.error;
  const computed = workerState.result;

  // Виртуализация. scrollTop читаем onScroll, не useEffect — для плавности
  // не критично, и так не отстаёт от scroll, к тому же экономим повторные RAF.
  const scrollContainerRef = useRef<HTMLDivElement | null>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const handleScroll = useCallback((e: UIEvent<HTMLDivElement>) => {
    setScrollTop(e.currentTarget.scrollTop);
  }, []);

  const totalRows = computed?.length ?? 0;
  const window = useMemo(
    () =>
      computed
        ? getVisibleWindow(computed, scrollTop, viewportHeight, rowHeight, overscan)
        : { start: 0, end: 0, offsetY: 0 },
    [computed, scrollTop, viewportHeight, rowHeight, overscan],
  );

  const visibleSlice = useMemo(
    () => (computed ? computed.slice(window.start, window.end) : []),
    [computed, window.start, window.end],
  );

  const totalChanges = useMemo(() => (computed ? countChanges(computed) : 0), [computed]);

  // Loading state. Приоритет: внешний isLoading > internal computing.
  const isLoadingNow = isLoading || computingDiff;

  // External error (props.error) или ошибка воркера. Внешняя имеет приоритет.
  const displayedError = error ?? computeError;

  // Отрисовка ветвей.
  if (isLoadingNow) {
    return (
      <section
        aria-label="Различия между версиями"
        aria-busy="true"
        data-testid="diff-viewer-root"
        className={cn(
          'flex items-center justify-center rounded-lg border border-border bg-bg p-6 text-sm text-fg-muted',
          className,
        )}
        style={{ minHeight: viewportHeight }}
      >
        <div className="flex flex-col items-center gap-3" data-testid="diff-viewer-loading">
          <Spinner size="lg" aria-hidden />
          <p>Подсчитываем diff…</p>
        </div>
      </section>
    );
  }

  if (displayedError !== undefined && displayedError !== null) {
    const message =
      displayedError instanceof Error ? displayedError.message : String(displayedError);
    return (
      <section
        aria-label="Различия между версиями"
        data-testid="diff-viewer-root"
        className={cn(
          'flex flex-col items-center justify-center gap-3 rounded-lg border border-danger/40 bg-bg p-6 text-sm',
          className,
        )}
        style={{ minHeight: viewportHeight }}
      >
        <div
          className="flex flex-col items-center gap-3"
          data-testid="diff-viewer-error"
          role="alert"
        >
          <p className="text-danger font-medium">Не удалось посчитать diff</p>
          {message ? <p className="text-fg-muted">{message}</p> : null}
          {onRetry ? (
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={onRetry}
              data-testid="diff-viewer-retry"
            >
              Повторить
            </Button>
          ) : null}
        </div>
      </section>
    );
  }

  if (paragraphs === undefined || paragraphs.length === 0 || totalRows === 0) {
    return (
      <section
        aria-label="Различия между версиями"
        data-testid="diff-viewer-root"
        className={cn(
          'flex flex-col items-center justify-center gap-2 rounded-lg border border-border bg-bg p-6 text-sm text-fg-muted',
          className,
        )}
        style={{ minHeight: viewportHeight }}
      >
        <div className="flex flex-col items-center gap-2" data-testid="diff-viewer-empty">
          <p className="text-base font-medium text-fg">Изменений нет</p>
          <p>Тексты двух версий идентичны.</p>
        </div>
      </section>
    );
  }

  // Главная ветка: toolbar + virtualized scroll-region.
  return (
    <section
      aria-label="Различия между версиями"
      data-testid="diff-viewer-root"
      className={cn('flex flex-col rounded-lg border border-border bg-bg', className)}
    >
      <DiffToolbar
        mode={mode}
        onModeChange={handleModeChange}
        totalParagraphs={totalRows}
        totalChanges={totalChanges}
      />
      <div
        ref={scrollContainerRef}
        role="region"
        aria-label="Различия между версиями"
        onScroll={handleScroll}
        className="overflow-auto"
        style={{ height: viewportHeight }}
        data-testid="diff-viewer-scroll"
      >
        <div
          style={{ height: totalRows * rowHeight, position: 'relative' }}
          data-testid="diff-viewer-spacer"
        >
          <div
            style={{
              transform: `translateY(${window.offsetY}px)`,
              position: 'absolute',
              top: 0,
              left: 0,
              right: 0,
            }}
          >
            {visibleSlice.map((computedRow) => (
              <DiffRow
                key={computedRow.paragraph.id}
                computed={computedRow}
                mode={mode}
                rowHeight={rowHeight}
              />
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}

export default DiffViewer;
