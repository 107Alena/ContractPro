// Column-конфигурация DocumentsTable (FE-TASK-044, §17.3 + §5.6.1 Pattern B).
//
// Колонки описаны data-driven, чтобы DocumentsTable оставалась компактной.
// «Действия» — отдельная колонка-кнопка, используется только при RBAC-доступе
// (useCan('contract.archive')). Скрываем через column-visibility (не удаляем
// определение целиком) — упрощает тесты и визуальную регрессию.
import { type ColumnDef } from '@tanstack/react-table';
import { Link } from 'react-router-dom';

import { type ContractSummary } from '@/entities/contract';
import { StatusBadge } from '@/entities/version';

export type DocumentStatusDisplay = 'ACTIVE' | 'ARCHIVED' | 'DELETED';

const STATUS_LABEL: Record<DocumentStatusDisplay, string> = {
  ACTIVE: 'Активный',
  ARCHIVED: 'В архиве',
  DELETED: 'Удалён',
};

function formatDate(iso?: string): string {
  if (!iso) return '—';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleDateString('ru-RU', { day: '2-digit', month: 'short', year: 'numeric' });
}

export interface ActionsRendererProps {
  contract: ContractSummary;
}

export interface DocumentsTableColumnsOptions {
  /** Рендер-функция для колонки «Действия»; если не передана — колонка не рендерится. */
  renderActions?: (props: ActionsRendererProps) => JSX.Element | null;
}

export function buildDocumentsTableColumns(
  opts: DocumentsTableColumnsOptions = {},
): ColumnDef<ContractSummary, unknown>[] {
  const columns: ColumnDef<ContractSummary, unknown>[] = [
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
              to={`/contracts/${encodeURIComponent(contract.contract_id)}`}
              className="text-fg hover:text-brand-600 focus-visible:text-brand-600 focus-visible:outline-none"
              data-testid={`documents-table-title-${contract.contract_id}`}
            >
              {title}
            </Link>
          );
        }
        return <span className="text-fg">{title}</span>;
      },
    },
    {
      id: 'processing_status',
      header: 'Статус обработки',
      accessorFn: (row) => row.processing_status ?? 'UPLOADED',
      cell: ({ row }) => <StatusBadge status={row.original.processing_status} />,
    },
    {
      id: 'status',
      header: 'Состояние',
      accessorFn: (row) => row.status ?? 'ACTIVE',
      cell: ({ row }) => {
        const s = (row.original.status ?? 'ACTIVE') as DocumentStatusDisplay;
        return <span className="text-fg-muted">{STATUS_LABEL[s]}</span>;
      },
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

export { STATUS_LABEL as DOCUMENT_STATUS_LABEL };
