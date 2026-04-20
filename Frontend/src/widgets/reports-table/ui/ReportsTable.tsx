// ReportsTable (FE-TASK-048) — таблица реестра отчётов на странице «Отчёты»
// (Figma 9, §17.4). Легче DocumentsTable: без виртуализации (реестр отчётов
// меньше реестра договоров — только READY/PARTIALLY_FAILED, серверная
// пагинация всё так же обеспечивает масштаб).
//
// Ключевые отличия:
//   - row-selection: клик по строке устанавливает выбранный contract_id
//     (для ReportDetailPanel справа). aria-selected/aria-current на строке.
//   - по умолчанию DESC по updated_at.
//   - empty/loading/error состояния — идентичны DocumentsTable по a11y-контракту.
import {
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  type SortingState,
  useReactTable,
} from '@tanstack/react-table';
import { type ReactNode, useMemo, useState } from 'react';

import { type ContractSummary } from '@/entities/contract';
import { Button, Spinner } from '@/shared/ui';

import { buildReportsTableColumns, type ReportRowActionsRendererProps } from '../model/columns';

export interface ReportsTableProps {
  items: readonly ContractSummary[];
  isLoading?: boolean;
  isFetching?: boolean;
  error?: unknown;
  renderRowActions?: (props: ReportRowActionsRendererProps) => JSX.Element | null;
  /** ID выбранной строки (для подсветки row-selection). */
  selectedId?: string | null;
  /** Коллбэк выбора строки — page поднимает в state для ReportDetailPanel. */
  onSelectRow?: (contract: ContractSummary) => void;
  onRetry?: () => void;
  emptyState?: ReactNode;
  filteredEmptyState?: ReactNode;
  hasActiveFilters?: boolean;
  ariaLabel?: string;
  testId?: string;
}

const INITIAL_SORTING: SortingState = [{ id: 'updated_at', desc: true }];

function LoadingBody({ columns }: { columns: number }): JSX.Element {
  return (
    <tbody>
      <tr>
        <td colSpan={columns} className="px-3 py-10 text-center">
          <div
            className="inline-flex items-center gap-2 text-sm text-fg-muted"
            data-testid="reports-table-loading"
          >
            <Spinner size="sm" aria-hidden="true" />
            <span>Загружаем список отчётов…</span>
          </div>
        </td>
      </tr>
    </tbody>
  );
}

function EmptyBody({ columns, children }: { columns: number; children: ReactNode }): JSX.Element {
  return (
    <tbody>
      <tr>
        <td colSpan={columns} className="px-3 py-10 text-center">
          {children}
        </td>
      </tr>
    </tbody>
  );
}

function ErrorBody({
  columns,
  message,
  onRetry,
}: {
  columns: number;
  message: string;
  onRetry?: () => void;
}): JSX.Element {
  return (
    <tbody>
      <tr>
        <td colSpan={columns} className="px-3 py-10 text-center">
          <div
            role="alert"
            data-testid="reports-table-error"
            className="flex flex-col items-center gap-2 text-sm"
          >
            <p className="font-medium text-danger">Не удалось загрузить список отчётов</p>
            <p className="text-fg-muted">{message}</p>
            {onRetry ? (
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={onRetry}
                data-testid="reports-table-retry"
              >
                Повторить
              </Button>
            ) : null}
          </div>
        </td>
      </tr>
    </tbody>
  );
}

function DefaultEmpty(): JSX.Element {
  return (
    <div
      className="flex flex-col items-center gap-2 text-fg-muted"
      data-testid="reports-table-empty"
    >
      <p className="text-sm font-medium text-fg">Готовых отчётов пока нет</p>
      <p className="text-xs">Отчёты появятся после завершения проверки договоров.</p>
    </div>
  );
}

function DefaultFilteredEmpty(): JSX.Element {
  return (
    <div
      className="flex flex-col items-center gap-2 text-fg-muted"
      data-testid="reports-table-empty-filtered"
    >
      <p className="text-sm font-medium text-fg">По вашему запросу отчёты не найдены</p>
      <p className="text-xs">Попробуйте изменить фильтры или поисковый запрос.</p>
    </div>
  );
}

export function ReportsTable({
  items,
  isLoading = false,
  isFetching = false,
  error,
  renderRowActions,
  selectedId = null,
  onSelectRow,
  onRetry,
  emptyState,
  filteredEmptyState,
  hasActiveFilters = false,
  ariaLabel = 'Список отчётов',
  testId = 'reports-table',
}: ReportsTableProps): JSX.Element {
  const [sorting, setSorting] = useState<SortingState>(INITIAL_SORTING);
  const columns = useMemo(
    () =>
      renderRowActions
        ? buildReportsTableColumns({ renderActions: renderRowActions })
        : buildReportsTableColumns(),
    [renderRowActions],
  );

  const data = useMemo(() => [...items], [items]);

  const table = useReactTable<ContractSummary>({
    data,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getRowId: (row, idx) => row.contract_id ?? `_row_${idx}`,
  });

  const visibleColumnsCount = table.getVisibleLeafColumns().length;
  const rows = table.getRowModel().rows;

  return (
    <section
      aria-label={ariaLabel}
      aria-busy={isLoading || isFetching ? 'true' : undefined}
      data-testid={testId}
      className="relative flex flex-col gap-2 rounded-md border border-border bg-bg"
    >
      {isFetching && !isLoading ? (
        <span
          aria-hidden="true"
          className="absolute right-3 top-3 z-10"
          data-testid={`${testId}-fetching`}
        >
          <Spinner size="sm" />
        </span>
      ) : null}
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-sm text-fg" role="table">
          <thead className="bg-bg-muted">
            {table.getHeaderGroups().map((hg) => (
              <tr key={hg.id} className="border-b border-border">
                {hg.headers.map((header) => {
                  const canSort = header.column.getCanSort();
                  const direction = header.column.getIsSorted();
                  const ariaSort =
                    canSort && direction === 'asc'
                      ? 'ascending'
                      : canSort && direction === 'desc'
                        ? 'descending'
                        : canSort
                          ? 'none'
                          : undefined;
                  return (
                    <th
                      key={header.id}
                      scope="col"
                      {...(ariaSort ? { 'aria-sort': ariaSort } : {})}
                      className="h-10 whitespace-nowrap px-3 text-left align-middle text-xs font-medium uppercase tracking-wide text-fg-muted"
                    >
                      {header.isPlaceholder ? null : canSort ? (
                        <button
                          type="button"
                          onClick={header.column.getToggleSortingHandler()}
                          aria-label={
                            typeof header.column.columnDef.header === 'string'
                              ? `Сортировать по «${header.column.columnDef.header}»`
                              : 'Сортировать'
                          }
                          className="inline-flex items-center gap-1 rounded-sm focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1"
                        >
                          {flexRender(header.column.columnDef.header, header.getContext())}
                          <span aria-hidden="true" className="text-fg-muted">
                            {direction === 'asc' ? '▲' : direction === 'desc' ? '▼' : '↕'}
                          </span>
                        </button>
                      ) : (
                        flexRender(header.column.columnDef.header, header.getContext())
                      )}
                    </th>
                  );
                })}
              </tr>
            ))}
          </thead>
          {isLoading ? (
            <LoadingBody columns={visibleColumnsCount} />
          ) : error != null ? (
            <ErrorBody
              columns={visibleColumnsCount}
              message={
                error instanceof Error
                  ? error.message
                  : typeof error === 'string'
                    ? error
                    : 'Произошла непредвиденная ошибка.'
              }
              {...(onRetry ? { onRetry } : {})}
            />
          ) : rows.length === 0 ? (
            <EmptyBody columns={visibleColumnsCount}>
              {hasActiveFilters
                ? (filteredEmptyState ?? <DefaultFilteredEmpty />)
                : (emptyState ?? <DefaultEmpty />)}
            </EmptyBody>
          ) : (
            <tbody>
              {rows.map((row) => {
                const isSelected = selectedId != null && row.original.contract_id === selectedId;
                const handleSelect = onSelectRow
                  ? (): void => onSelectRow(row.original)
                  : undefined;
                const handleKeyDown = handleSelect
                  ? (e: React.KeyboardEvent<HTMLTableRowElement>): void => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        handleSelect();
                      }
                    }
                  : undefined;
                return (
                  <tr
                    key={row.id}
                    data-testid={`reports-table-row-${row.id}`}
                    aria-selected={isSelected ? 'true' : 'false'}
                    aria-current={isSelected ? 'true' : undefined}
                    tabIndex={handleSelect ? 0 : undefined}
                    role={handleSelect ? 'row' : undefined}
                    onClick={handleSelect}
                    onKeyDown={handleKeyDown}
                    className={`border-b border-border last:border-b-0 ${
                      handleSelect ? 'cursor-pointer' : ''
                    } ${
                      isSelected
                        ? 'bg-[color-mix(in_srgb,var(--color-brand-600)_8%,transparent)]'
                        : 'hover:bg-bg-muted'
                    }`}
                  >
                    {row.getVisibleCells().map((cell) => (
                      <td
                        key={cell.id}
                        className="whitespace-nowrap px-3 py-2.5 align-middle text-sm text-fg"
                        // Клик по action-кнопкам не должен поднимать row-select.
                        onClick={
                          cell.column.id === 'actions'
                            ? (e): void => e.stopPropagation()
                            : undefined
                        }
                      >
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    ))}
                  </tr>
                );
              })}
            </tbody>
          )}
        </table>
      </div>
    </section>
  );
}
