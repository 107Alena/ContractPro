// ChangesTable — клиентская DataTable с тремя колонками: Тип, Путь/Узел,
// Содержимое (truncate). Принимает текстовые и структурные изменения,
// фильтрует по ChangesFilter ПЕРЕД построением rowModel'а TanStack Table
// (так пагинация уже видит итоговый набор).
//
// 'high-risk' — демо-эвристика: считаем «высокорисковыми» все removed
// изменения, поскольку API не возвращает risk-level в diff. Это явно
// указано в README/прогрессе и может быть заменено на реальный сигнал,
// когда орекстратор начнёт прокидывать risk-flag в diff-payload.
import type { ColumnDef } from '@tanstack/react-table';
import { useMemo } from 'react';

import type {
  VersionDiffStructuralChange,
  VersionDiffTextChange,
} from '@/features/comparison-start';
import { cn } from '@/shared/lib/cn';
import { Badge, type BadgeProps } from '@/shared/ui/badge';
import { Spinner } from '@/shared/ui/spinner';
import { DataTable, DataTableContent, DataTablePagination } from '@/shared/ui/table';

import type { ChangesFilter } from '../model/types';

export interface ChangesTableProps {
  changes: readonly VersionDiffTextChange[];
  structuralChanges?: readonly VersionDiffStructuralChange[];
  filter?: ChangesFilter;
  isLoading?: boolean;
  className?: string;
}

interface UnifiedChange {
  id: string;
  source: 'textual' | 'structural';
  type: 'added' | 'removed' | 'modified' | 'moved' | 'unknown';
  pathOrNode: string;
  content: string;
}

const TYPE_LABEL: Record<UnifiedChange['type'], string> = {
  added: 'Добавлено',
  removed: 'Удалено',
  modified: 'Изменено',
  moved: 'Перемещено',
  unknown: '—',
};

const TYPE_VARIANT: Record<UnifiedChange['type'], NonNullable<BadgeProps['variant']>> = {
  added: 'success',
  removed: 'danger',
  modified: 'warning',
  moved: 'brand',
  unknown: 'neutral',
};

const MAX_CONTENT = 200;

function truncate(text: string, max = MAX_CONTENT): string {
  return text.length <= max ? text : `${text.slice(0, max - 1)}…`;
}

function describeText(change: VersionDiffTextChange): string {
  const oldText = change.old_text ?? '';
  const newText = change.new_text ?? '';
  if (change.type === 'added') return newText;
  if (change.type === 'removed') return oldText;
  // modified
  if (oldText && newText) return `${oldText} → ${newText}`;
  return newText || oldText;
}

function describeStructural(change: VersionDiffStructuralChange): string {
  const old_ = change.old_value ? JSON.stringify(change.old_value) : '';
  const new_ = change.new_value ? JSON.stringify(change.new_value) : '';
  if (change.type === 'added') return new_;
  if (change.type === 'removed') return old_;
  if (old_ && new_) return `${old_} → ${new_}`;
  return new_ || old_;
}

function normalizeType(t: string | undefined): UnifiedChange['type'] {
  if (t === 'added' || t === 'removed' || t === 'modified' || t === 'moved') return t;
  return 'unknown';
}

function buildUnified(
  textChanges: readonly VersionDiffTextChange[],
  structuralChanges: readonly VersionDiffStructuralChange[],
): UnifiedChange[] {
  const out: UnifiedChange[] = [];
  textChanges.forEach((change, index) => {
    out.push({
      id: `t-${index}-${change.path ?? 'no-path'}`,
      source: 'textual',
      type: normalizeType(change.type),
      pathOrNode: change.path ?? '—',
      content: truncate(describeText(change)),
    });
  });
  structuralChanges.forEach((change, index) => {
    out.push({
      id: `s-${index}-${change.node_id ?? 'no-node'}`,
      source: 'structural',
      type: normalizeType(change.type),
      pathOrNode: change.node_id ?? '—',
      content: truncate(describeStructural(change)),
    });
  });
  return out;
}

function applyFilter(rows: UnifiedChange[], filter: ChangesFilter): UnifiedChange[] {
  switch (filter) {
    case 'all':
      return rows;
    case 'textual':
      return rows.filter((r) => r.source === 'textual');
    case 'structural':
      return rows.filter((r) => r.source === 'structural');
    case 'high-risk':
      return rows.filter((r) => r.type === 'removed');
  }
}

export function ChangesTable({
  changes,
  structuralChanges,
  filter = 'all',
  isLoading = false,
  className,
}: ChangesTableProps): JSX.Element {
  const rows = useMemo(
    () => applyFilter(buildUnified(changes, structuralChanges ?? []), filter),
    [changes, structuralChanges, filter],
  );

  const columns = useMemo<ColumnDef<UnifiedChange>[]>(
    () => [
      {
        id: 'type',
        header: 'Тип',
        accessorKey: 'type',
        cell: ({ row }) => {
          const t = row.original.type;
          return (
            <Badge variant={TYPE_VARIANT[t]} data-testid={`changes-row-type-${t}`}>
              {TYPE_LABEL[t]}
            </Badge>
          );
        },
      },
      {
        id: 'path',
        header: 'Путь / Узел',
        accessorKey: 'pathOrNode',
        cell: ({ row }) => (
          <span className="font-mono text-xs text-fg-muted">{row.original.pathOrNode}</span>
        ),
      },
      {
        id: 'content',
        header: 'Содержимое',
        accessorKey: 'content',
        cell: ({ row }) => (
          <span className="block max-w-[40ch] truncate text-fg" title={row.original.content}>
            {row.original.content || <span className="text-fg-muted">—</span>}
          </span>
        ),
      },
    ],
    [],
  );

  return (
    <div data-testid="changes-table" className={cn('flex flex-col gap-2', className)}>
      <DataTable<UnifiedChange>
        data={rows}
        columns={columns}
        isLoading={isLoading}
        getRowId={(r) => r.id}
        loadingState={
          <div className="flex items-center justify-center gap-2">
            <Spinner size="sm" aria-hidden />
            <span>Загрузка изменений…</span>
          </div>
        }
        emptyState={
          <div className="flex flex-col items-center gap-1 text-fg-muted">
            <p className="text-sm font-medium text-fg">Нет изменений по выбранному фильтру</p>
            <p className="text-xs">Попробуйте переключиться на «Все».</p>
          </div>
        }
      >
        <DataTableContent />
        <DataTablePagination pageSizeOptions={[10, 25, 50]} />
      </DataTable>
    </div>
  );
}
