// Один сегмент внутри строки diff.
// insert — зелёный фон (success), delete — красный с line-through (danger),
// equal — обычный текст наследует цвет fg из родителя.
import { memo } from 'react';

import { cn } from '@/shared/lib/cn';

import type { DiffSegment as DiffSegmentModel } from '../model/types';

export interface DiffSegmentProps {
  segment: DiffSegmentModel;
  /** Hide content of opposite-side segments в side-by-side колонках (visual диета). */
  hidden?: boolean;
}

function DiffSegmentImpl({ segment, hidden = false }: DiffSegmentProps) {
  if (hidden) return null;
  if (segment.kind === 'insert') {
    return (
      <span
        className={cn('rounded-sm bg-success/15 px-0.5 text-success')}
        data-segment-kind="insert"
      >
        {segment.text}
      </span>
    );
  }
  if (segment.kind === 'delete') {
    return (
      <span
        className={cn('rounded-sm bg-danger/15 px-0.5 text-danger line-through')}
        data-segment-kind="delete"
      >
        {segment.text}
      </span>
    );
  }
  return <span data-segment-kind="equal">{segment.text}</span>;
}

export const DiffSegmentView = memo(DiffSegmentImpl);
