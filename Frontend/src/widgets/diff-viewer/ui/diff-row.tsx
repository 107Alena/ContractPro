// Одна строка-параграф diff. Memoized — родитель пере-рендерится при скролле,
// но props (computed, mode, rowHeight) для большинства строк стабильны.
//
// Side-by-side: 2 колонки. Слева показываем сегменты equal+delete (то, что
// было), справа equal+insert (то, что стало). Это даёт визуальное соответствие
// двух версий и совпадает с привычными tools (GitHub, Bitbucket).
//
// Inline: одна колонка. Слева маркер +/-/~. Сегменты идут подряд.
import { memo } from 'react';

import { cn } from '@/shared/lib/cn';

import type { ComputedDiffParagraph, DiffMode } from '../model/types';
import { DiffSegmentView } from './diff-segment';

export interface DiffRowProps {
  computed: ComputedDiffParagraph;
  mode: DiffMode;
  rowHeight: number;
}

const STATUS_MARKER: Record<ComputedDiffParagraph['paragraph']['status'], string> = {
  added: '+',
  removed: '−',
  modified: '~',
  unchanged: ' ',
};

const STATUS_TONE: Record<ComputedDiffParagraph['paragraph']['status'], string> = {
  added: 'text-success',
  removed: 'text-danger',
  modified: 'text-warning',
  unchanged: 'text-fg-muted',
};

function DiffRowImpl({ computed, mode, rowHeight }: DiffRowProps) {
  const { paragraph, segments } = computed;

  if (mode === 'side-by-side') {
    return (
      <div
        className="grid grid-cols-2 gap-2 border-b border-border px-3 py-2"
        style={{ minHeight: rowHeight }}
        data-testid="diff-row"
        data-paragraph-id={paragraph.id}
        data-status={paragraph.status}
      >
        <div className="whitespace-pre-wrap break-words text-sm text-fg" lang="ru">
          {segments.map((seg, i) => (
            <DiffSegmentView
              // index-key безопасен: segments стабильны для конкретного computed
              // (мы пересчитываем целиком при смене paragraphs).
              key={`l-${i}`}
              segment={seg}
              hidden={seg.kind === 'insert'}
            />
          ))}
        </div>
        <div className="whitespace-pre-wrap break-words text-sm text-fg" lang="ru">
          {segments.map((seg, i) => (
            <DiffSegmentView key={`r-${i}`} segment={seg} hidden={seg.kind === 'delete'} />
          ))}
        </div>
      </div>
    );
  }

  // inline
  return (
    <div
      className="flex gap-2 border-b border-border px-3 py-2"
      style={{ minHeight: rowHeight }}
      data-testid="diff-row"
      data-paragraph-id={paragraph.id}
      data-status={paragraph.status}
    >
      <span
        aria-hidden
        className={cn(
          'mt-0.5 select-none font-mono text-xs font-semibold tabular-nums',
          STATUS_TONE[paragraph.status],
        )}
      >
        {STATUS_MARKER[paragraph.status]}
      </span>
      <div className="min-w-0 flex-1 whitespace-pre-wrap break-words text-sm text-fg" lang="ru">
        {segments.map((seg, i) => (
          <DiffSegmentView key={`i-${i}`} segment={seg} />
        ))}
      </div>
    </div>
  );
}

export const DiffRow = memo(DiffRowImpl);
