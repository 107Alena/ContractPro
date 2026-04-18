// RecentChecksTable — виджет «Последние проверки» на dashboard (§17.4).
//
// Композиция DataTable (FE-TASK-021) с 5 последними договорами из
// /contracts?size=5. SSE-обновления статусов — через useEventStream на
// уровне страницы; setQueryData(qk.contracts.status(...)) не влияет на
// список /contracts, но сама /contracts перезапрашивается при invalidate.
// Для стабильного UX при SSE-статусе мы отображаем processing_status из
// ContractSummary (snapshot по времени /contracts) — для мгновенного
// real-time status отдельный useQuery по status-ключу в FE-TASK-044.
import type { ColumnDef } from '@tanstack/react-table';
import { useMemo } from 'react';
import { Link } from 'react-router-dom';

import { type ContractSummary, viewStatus } from '@/entities/contract';
import { Badge, buttonVariants, DataTable, DataTableContent } from '@/shared/ui';

export interface RecentChecksTableProps {
  items?: readonly ContractSummary[] | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

function formatDate(iso?: string): string {
  if (!iso) return '—';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleDateString('ru-RU', { day: '2-digit', month: 'short', year: 'numeric' });
}

function useColumns(): ColumnDef<ContractSummary, unknown>[] {
  return useMemo<ColumnDef<ContractSummary, unknown>[]>(
    () => [
      {
        id: 'title',
        header: 'Название',
        accessorFn: (row) => row.title ?? 'Без названия',
        cell: ({ row }) => {
          const contract = row.original;
          const title = contract.title ?? 'Без названия';
          if (contract.contract_id) {
            return (
              <Link
                to={`/contracts/${contract.contract_id}`}
                className="text-fg hover:text-brand-600 focus-visible:text-brand-600 focus-visible:outline-none"
              >
                {title}
              </Link>
            );
          }
          return <span className="text-fg">{title}</span>;
        },
      },
      {
        id: 'status',
        header: 'Статус',
        accessorFn: (row) => row.processing_status ?? 'UPLOADED',
        cell: ({ row }) => {
          const view = viewStatus(row.original.processing_status);
          return <Badge variant={view.tone}>{view.label}</Badge>;
        },
      },
      {
        id: 'updated_at',
        header: 'Обновлён',
        accessorFn: (row) => row.updated_at ?? row.created_at ?? '',
        cell: ({ row }) => (
          <span className="text-fg-muted">
            {formatDate(row.original.updated_at ?? row.original.created_at)}
          </span>
        ),
      },
    ],
    [],
  );
}

export function RecentChecksTable({
  items,
  isLoading,
  error,
}: RecentChecksTableProps): JSX.Element {
  const columns = useColumns();
  const data = items ? Array.from(items) : [];

  return (
    <section
      aria-label="Последние проверки"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header className="flex items-center justify-between gap-2">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Последние проверки
        </h2>
        <Link to="/contracts" className={buttonVariants({ variant: 'ghost', size: 'sm' })}>
          Смотреть все
        </Link>
      </header>

      <DataTable<ContractSummary>
        data={data}
        columns={columns}
        isLoading={Boolean(isLoading) && data.length === 0}
        error={error}
        getRowId={(row, index) => row.contract_id ?? row.title ?? `_missing_${index}`}
        emptyState={
          <div className="flex flex-col items-center gap-2 py-6 text-fg-muted">
            <p>Ещё не было проверок.</p>
            <Link
              to="/contracts/new"
              className={buttonVariants({ variant: 'primary', size: 'sm' })}
            >
              Загрузить первый договор
            </Link>
          </div>
        }
        errorState={
          <p role="alert" className="py-6 text-center text-danger">
            Не удалось загрузить список.
          </p>
        }
      >
        <DataTableContent />
      </DataTable>
    </section>
  );
}
