// RecentChecksTable — «Недавние проверки» на dashboard (Figma 84:2 → 90:2).
//
// Композиция DataTable с 5 последними договорами из /contracts?size=5.
// Колонки Тип и Риск с ORCH-TASK-056 рендерят реальные contract_type / risk_level
// из ContractSummary; «—» остаётся только при null (договор не проанализирован).
// SSE-обновления статусов — через useEventStream на уровне страницы; на /contracts
// snapshot не инвалидируется, отображаем processing_status из ContractSummary.
import type { ColumnDef } from '@tanstack/react-table';
import { useMemo } from 'react';
import { Link } from 'react-router-dom';

import { type ContractSummary, contractTypeLabel, viewStatus } from '@/entities/contract';
import { RiskBadge } from '@/entities/risk';
import { Badge, buttonVariants, Card, DataTable, DataTableContent } from '@/shared/ui';

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

function Dash(): JSX.Element {
  return <span className="text-fg-disabled">—</span>;
}

function useColumns(): ColumnDef<ContractSummary, unknown>[] {
  return useMemo<ColumnDef<ContractSummary, unknown>[]>(
    () => [
      {
        id: 'title',
        header: 'Документ',
        accessorFn: (row) => row.title ?? 'Без названия',
        cell: ({ row }) => {
          const contract = row.original;
          const title = contract.title ?? 'Без названия';
          if (contract.contract_id) {
            return (
              <Link
                to={`/contracts/${contract.contract_id}`}
                className="font-medium text-fg hover:text-brand-600 focus-visible:text-brand-600 focus-visible:outline-none"
              >
                {title}
              </Link>
            );
          }
          return <span className="font-medium text-fg">{title}</span>;
        },
      },
      {
        // Тип договора из ContractSummary.contract_type (ORCH-TASK-056); «—» при null.
        id: 'type',
        header: 'Тип',
        accessorFn: (row) => contractTypeLabel(row.contract_type) ?? '',
        cell: ({ row }) => {
          const label = contractTypeLabel(row.original.contract_type);
          // text-fg — одинаковая эмфаза с колонкой «Тип» в DocumentsTable (та же данность).
          return label ? <span className="text-fg">{label}</span> : <Dash />;
        },
      },
      {
        id: 'updated_at',
        header: 'Дата',
        accessorFn: (row) => row.updated_at ?? row.created_at ?? '',
        cell: ({ row }) => (
          <span className="text-fg-muted">
            {formatDate(row.original.updated_at ?? row.original.created_at)}
          </span>
        ),
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
        // Уровень риска из ContractSummary.risk_level (ORCH-TASK-056); «—» при null.
        id: 'risk',
        header: 'Риск',
        accessorFn: (row) => row.risk_level ?? '',
        cell: ({ row }) => {
          const level = row.original.risk_level;
          return level ? <RiskBadge level={level} /> : <Dash />;
        },
      },
      {
        id: 'actions',
        header: '',
        enableSorting: false,
        cell: ({ row }) => {
          const id = row.original.contract_id;
          if (!id) return null;
          return (
            <Link
              to={`/contracts/${id}`}
              className="text-13 font-medium text-brand-600 hover:text-brand-500 focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2"
            >
              Открыть
            </Link>
          );
        },
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
    <Card aria-label="Недавние проверки" className="flex flex-col gap-4 px-6 py-[22px]">
      <header className="flex items-center justify-between gap-2">
        <h2 className="text-17 font-semibold text-fg">Недавние проверки</h2>
        <Link
          to="/contracts"
          className="text-13 font-medium text-brand-600 hover:text-brand-500 focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2"
        >
          Все проверки →
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
    </Card>
  );
}
