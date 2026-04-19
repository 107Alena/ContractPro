// KeyDiffsBySection — список топ-секций договора с бейджами +X / -Y / ~Z.
// Источник — groupBySection (lib/group-by-section.ts).
import { cn } from '@/shared/lib/cn';
import { Badge } from '@/shared/ui/badge';

import type { SectionDiffSummary } from '../model/types';

export interface KeyDiffsBySectionProps {
  sections: readonly SectionDiffSummary[];
  className?: string;
}

export function KeyDiffsBySection({ sections, className }: KeyDiffsBySectionProps): JSX.Element {
  return (
    <section
      aria-label="Ключевые изменения по разделам"
      data-testid="key-diffs-by-section"
      className={cn('flex flex-col gap-3 rounded-lg border border-border bg-bg p-4', className)}
    >
      <h3 className="text-sm font-semibold text-fg">Ключевые изменения по разделам</h3>
      {sections.length === 0 ? (
        <p className="text-sm text-fg-muted" data-testid="key-diffs-empty">
          Нет данных по разделам
        </p>
      ) : (
        <ul className="flex flex-col gap-1.5">
          {sections.map((section) => (
            <li
              key={section.section}
              data-testid={`key-diffs-row-${section.section}`}
              className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border px-3 py-2"
            >
              <span className="font-mono text-xs text-fg">{section.section}</span>
              <span className="flex flex-wrap gap-1">
                <Badge variant="success" data-testid="key-diffs-added">
                  +{section.added}
                </Badge>
                <Badge variant="danger" data-testid="key-diffs-removed">
                  -{section.removed}
                </Badge>
                <Badge variant="warning" data-testid="key-diffs-modified">
                  ~{section.modified}
                </Badge>
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
