// Toolbar диффа: переключатель режима side-by-side / inline + счётчики.
// Segmented control из 2 кнопок (role="toolbar" + aria-pressed) — это
// доступнее, чем select, и привычнее для diff-viewer'ов.
import { memo } from 'react';

import { cn } from '@/shared/lib/cn';

import type { DiffMode } from '../model/types';

export interface DiffToolbarProps {
  mode: DiffMode;
  onModeChange: (mode: DiffMode) => void;
  totalParagraphs: number;
  totalChanges: number;
}

interface ModeOption {
  value: DiffMode;
  label: string;
}

const MODE_OPTIONS: readonly ModeOption[] = [
  { value: 'side-by-side', label: 'Бок о бок' },
  { value: 'inline', label: 'В одну колонку' },
];

function DiffToolbarImpl({ mode, onModeChange, totalParagraphs, totalChanges }: DiffToolbarProps) {
  return (
    <div
      role="toolbar"
      aria-label="Управление отображением различий"
      data-testid="diff-toolbar"
      className="flex flex-wrap items-center justify-between gap-3 border-b border-border bg-bg-muted px-3 py-2"
    >
      <div
        className="inline-flex overflow-hidden rounded-md border border-border bg-bg"
        data-testid="diff-toolbar-mode-toggle"
      >
        {MODE_OPTIONS.map((option) => {
          const isActive = option.value === mode;
          return (
            <button
              key={option.value}
              type="button"
              aria-pressed={isActive}
              onClick={() => onModeChange(option.value)}
              data-mode={option.value}
              className={cn(
                'px-3 py-1.5 text-sm transition-colors',
                'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1',
                isActive
                  ? 'bg-brand-50 text-brand-600 font-medium'
                  : 'text-fg-muted hover:bg-bg-muted',
              )}
            >
              {option.label}
            </button>
          );
        })}
      </div>
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-fg-muted">
        <span>
          Параграфов: <span className="font-medium text-fg">{totalParagraphs}</span>
        </span>
        <span>
          Изменений: <span className="font-medium text-fg">{totalChanges}</span>
        </span>
      </div>
    </div>
  );
}

export const DiffToolbar = memo(DiffToolbarImpl);
