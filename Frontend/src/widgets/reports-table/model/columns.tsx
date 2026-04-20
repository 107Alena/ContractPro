// Колонки ReportsTable (FE-TASK-048).
//
// Отличия от DocumentsTable:
//   - Колонка «Обновлён» отсортирована по умолчанию (DESC): в реестре отчётов
//     пользователя сверху всегда свежие результаты.
//   - Нет колонки «Состояние» (DocumentStatus) — на странице отчётов сервер
//     всегда фильтрует status=ACTIVE (см. use-reports-list-query.ts).
//   - «Действия»: одна кнопка «Открыть» с ролью row-selector + опционально
//     renderActions от page для «Поделиться/Скачать».
import { type ColumnDef } from '@tanstack/react-table';
import { Link } from 'react-router-dom';

import { type ContractSummary } from '@/entities/contract';
import { StatusBadge } from '@/entities/version';

function formatDate(iso?: string): string {
  if (!iso) return '—';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleDateString('ru-RU', { day: '2-digit', month: 'short', year: 'numeric' });
}

export interface ReportRowActionsRendererProps {
  contract: ContractSummary;
}

export interface ReportsTableColumnsOptions {
  renderActions?: (props: ReportRowActionsRendererProps) => JSX.Element | null;
}

export function buildReportsTableColumns(
  opts: ReportsTableColumnsOptions = {},
): ColumnDef<ContractSummary, unknown>[] {
  const columns: ColumnDef<ContractSummary, unknown>[] = [
    {
      id: 'title',
      header: 'Название',
      accessorFn: (row) => row.title ?? 'Без названия',
      cell: ({ row }) => {
        const contract = row.original;
        const title = contract.title ?? 'Без названия';
        if (!contract.contract_id) {
          return <span className="text-fg">{title}</span>;
        }
        return (
          <Link
            to={`/contracts/${encodeURIComponent(contract.contract_id)}`}
            className="text-fg hover:text-brand-600 focus-visible:text-brand-600 focus-visible:outline-none"
            data-testid={`reports-table-title-${contract.contract_id}`}
          >
            {title}
          </Link>
        );
      },
    },
    {
      id: 'processing_status',
      header: 'Статус обработки',
      accessorFn: (row) => row.processing_status ?? 'UPLOADED',
      cell: ({ row }) => <StatusBadge status={row.original.processing_status} />,
    },
    {
      id: 'current_version_number',
      header: 'Версия',
      accessorFn: (row) => row.current_version_number ?? 0,
      cell: ({ row }) => {
        const n = row.original.current_version_number;
        return <span className="text-fg-muted">{n != null ? `v${n}` : '—'}</span>;
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
      sortDescFirst: true,
    },
  ];

  if (opts.renderActions) {
    const renderActions = opts.renderActions;
    columns.push({
      id: 'actions',
      header: 'Действия',
      enableSorting: false,
      cell: ({ row }) => renderActions({ contract: row.original }),
    });
  }

  return columns;
}
