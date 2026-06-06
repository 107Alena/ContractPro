// Column-конфигурация DocumentsTable (FE-TASK-044/058, Figma 201:2, §5.6.1 Pattern B).
//
// Колонки = Figma: Документ (PDF-бейдж + название + status-derived подпись) /
// Тип / Дата проверки / Статус / Риск / Версии / Действия.
// Тип договора (contract_type) и уровень риска (risk_level) с ORCH-TASK-056 входят
// в ContractSummary → колонки «Тип»/«Риск» рендерят реальные данные. «—» остаётся
// ТОЛЬКО когда поле null (договор не проанализирован / тип не выявлен) — не выдумываем.
// Подпись под названием выводится из РЕАЛЬНОГО processing_status. «Действия» —
// отдельная колонка, рендерится только при RBAC-доступе (renderActions от page'а).
// Сортировка — server-side (page-уровень SortControl → API sort/order); колонки
// не сортируются кликом (enableSorting:false), чтобы per-page reorder не
// противоречил глобальному серверному порядку.
import { type ColumnDef } from '@tanstack/react-table';
import { Link } from 'react-router-dom';

import { type ContractSummary, contractTypeLabel, viewStatus } from '@/entities/contract';
import { RiskBadge } from '@/entities/risk';
import { StatusBadge } from '@/entities/version';

function formatDate(iso?: string): string {
  if (!iso) return '—';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleDateString('ru-RU', { day: '2-digit', month: 'short', year: 'numeric' });
}

// Подпись под названием — из реального статуса обработки (honest, не выдуманное).
function statusSubline(status: ContractSummary['processing_status']): string {
  switch (viewStatus(status).bucket) {
    case 'ready':
      return 'Отчёт готов';
    case 'in_progress':
      return 'Анализ рисков…';
    case 'awaiting':
      return 'Требует подтверждения типа';
    case 'failed':
      return 'Обработка завершилась с ошибкой';
    default:
      return 'В очереди на проверку';
  }
}

function Dash(): JSX.Element {
  return <span className="text-fg-disabled">—</span>;
}

function PdfBadge(): JSX.Element {
  return (
    <span
      aria-hidden="true"
      className="inline-flex h-5 shrink-0 items-center rounded-[4px] bg-bg-muted px-1 text-11 font-semibold text-fg-subtle"
    >
      PDF
    </span>
  );
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
      header: 'Документ',
      accessorFn: (row) => row.title ?? 'Без названия',
      cell: ({ row }) => {
        const contract = row.original;
        const title = contract.title ?? 'Без названия';
        return (
          <div className="flex items-start gap-3">
            <PdfBadge />
            <div className="flex min-w-0 flex-col">
              {contract.contract_id ? (
                <Link
                  to={`/contracts/${encodeURIComponent(contract.contract_id)}`}
                  className="truncate font-medium text-fg hover:text-brand-600 focus-visible:text-brand-600 focus-visible:outline-none"
                  data-testid={`documents-table-title-${contract.contract_id}`}
                >
                  {title}
                </Link>
              ) : (
                <span className="truncate font-medium text-fg">{title}</span>
              )}
              <span className="truncate text-13 text-fg-subtle">
                {statusSubline(contract.processing_status)}
              </span>
            </div>
          </div>
        );
      },
    },
    {
      // Тип договора из ContractSummary.contract_type (ORCH-TASK-056); «—» при null.
      id: 'type',
      header: 'Тип',
      enableSorting: false,
      accessorFn: (row) => contractTypeLabel(row.contract_type) ?? '',
      cell: ({ row }) => {
        const label = contractTypeLabel(row.original.contract_type);
        return label ? <span className="text-fg">{label}</span> : <Dash />;
      },
    },
    {
      id: 'updated_at',
      header: 'Дата проверки',
      accessorFn: (row) => row.updated_at ?? row.created_at ?? '',
      cell: ({ row }) => (
        <span className="whitespace-nowrap text-fg-muted">
          {formatDate(row.original.updated_at ?? row.original.created_at)}
        </span>
      ),
    },
    {
      id: 'processing_status',
      header: 'Статус',
      accessorFn: (row) => row.processing_status ?? 'UPLOADED',
      cell: ({ row }) => <StatusBadge status={row.original.processing_status} />,
    },
    {
      // Уровень риска из ContractSummary.risk_level (ORCH-TASK-056); «—» при null
      // (нет READY-результата). RiskBadge без tooltip — в плотной таблице (см. risk-badge.tsx).
      id: 'risk',
      header: 'Риск',
      enableSorting: false,
      accessorFn: (row) => row.risk_level ?? '',
      cell: ({ row }) => {
        const level = row.original.risk_level;
        return level ? <RiskBadge level={level} /> : <Dash />;
      },
    },
    {
      id: 'current_version_number',
      header: 'Версии',
      accessorFn: (row) => row.current_version_number ?? 0,
      cell: ({ row }) => {
        const n = row.original.current_version_number;
        return <span className="text-fg-muted">{n != null ? `v${n}` : '—'}</span>;
      },
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
