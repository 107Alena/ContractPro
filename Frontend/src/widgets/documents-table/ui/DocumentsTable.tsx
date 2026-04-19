// DocumentsTable (FE-TASK-044) — основной виджет списка договоров на странице «Документы».
//
// Архитектура: §17.3/§17.4 + §11.2 (виртуализация — только >100 строк),
// §5.6.1 Pattern B (скрытие действий для BUSINESS_USER через <Can>).
//
// Реализация:
//   - `useReactTable` в ручном (manualPagination/Sorting=false — сортировка
//     client-side по текущей странице; server-side пагинация наверху в page).
//   - Колонка «Действия» добавляется только если вызывающая сторона передала
//     renderRowActions (page проверяет useCan('contract.archive')).
//   - Виртуализация через @tanstack/react-virtual включается, когда видимых
//     строк ≥ VIRTUALIZATION_THRESHOLD. До порога — обычный <tbody>, чтобы
//     Ctrl+F/скриншоты/accessibility работали из коробки.
//   - Fixed row height (ROW_HEIGHT_PX=56): виртуализация требует известного
//     размера; дизайн «Документы» использует строки одной высоты.
import {
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  type Row,
  type SortingState,
  useReactTable,
} from '@tanstack/react-table';
import { useVirtualizer } from '@tanstack/react-virtual';
import { type ReactNode, useCallback, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';

import { type ContractSummary } from '@/entities/contract';
import { Button, buttonVariants, Spinner } from '@/shared/ui';

import { type ActionsRendererProps, buildDocumentsTableColumns } from '../model/columns';

export const ROW_HEIGHT_PX = 56;
export const VIRTUALIZATION_THRESHOLD = 50;

export interface DocumentsTableProps {
  items: readonly ContractSummary[];
  isLoading?: boolean;
  /** true → блёклая наложенная маска поверх данных (при переходе страниц). */
  isFetching?: boolean;
  error?: unknown;
  /** Если передан — рендерит колонку «Действия» для каждой строки. */
  renderRowActions?: (props: ActionsRendererProps) => JSX.Element | null;
  /** Коллбэк повторной попытки из error-state. */
  onRetry?: () => void;
  /** Empty-state для случая «у пользователя нет договоров» (без активных фильтров). */
  emptyState?: ReactNode;
  /** Empty-state при активных фильтрах/поиске — другая копия CTA. */
  filteredEmptyState?: ReactNode;
  /** true — текущее состояние: фильтры/поиск активны, empty → filteredEmptyState. */
  hasActiveFilters?: boolean;
  /** Заголовок для aria-label корневой секции. */
  ariaLabel?: string;
  /** data-testid на секцию (для e2e). */
  testId?: string;
}

function LoadingBody({ columns }: { columns: number }): JSX.Element {
  return (
    <tbody>
      <tr>
        <td colSpan={columns} className="px-3 py-10 text-center">
          <div
            className="inline-flex items-center gap-2 text-sm text-fg-muted"
            data-testid="documents-table-loading"
          >
            <Spinner size="sm" aria-hidden="true" />
            <span>Загружаем список договоров…</span>
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
            data-testid="documents-table-error"
            className="flex flex-col items-center gap-2 text-sm"
          >
            <p className="font-medium text-danger">Не удалось загрузить список</p>
            <p className="text-fg-muted">{message}</p>
            {onRetry ? (
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={onRetry}
                data-testid="documents-table-retry"
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
      className="flex flex-col items-center gap-3 text-fg-muted"
      data-testid="documents-table-empty"
    >
      <p className="text-sm font-medium text-fg">Ещё нет договоров</p>
      <p className="text-xs">Начните с загрузки первого документа.</p>
      <Link to="/contracts/new" className={buttonVariants({ variant: 'primary', size: 'sm' })}>
        Загрузить договор
      </Link>
    </div>
  );
}

function DefaultFilteredEmpty(): JSX.Element {
  return (
    <div
      className="flex flex-col items-center gap-2 text-fg-muted"
      data-testid="documents-table-empty-filtered"
    >
      <p className="text-sm font-medium text-fg">По вашему запросу ничего не найдено</p>
      <p className="text-xs">Попробуйте изменить фильтры или поисковый запрос.</p>
    </div>
  );
}

export function DocumentsTable({
  items,
  isLoading = false,
  isFetching = false,
  error,
  renderRowActions,
  onRetry,
  emptyState,
  filteredEmptyState,
  hasActiveFilters = false,
  ariaLabel = 'Список договоров',
  testId = 'documents-table',
}: DocumentsTableProps): JSX.Element {
  const [sorting, setSorting] = useState<SortingState>([]);
  const columns = useMemo(() => {
    // renderActions передаётся только если вызывающий сам решил показать
    // колонку (RBAC-проверка — на уровне page).
    return renderRowActions
      ? buildDocumentsTableColumns({ renderActions: renderRowActions })
      : buildDocumentsTableColumns();
  }, [renderRowActions]);

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
  const shouldVirtualize = rows.length >= VIRTUALIZATION_THRESHOLD;

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
      {shouldVirtualize ? (
        <VirtualizedBody
          rows={rows}
          columns={visibleColumnsCount}
          headerGroups={table.getHeaderGroups()}
        />
      ) : (
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
                {rows.map((row) => (
                  <tr
                    key={row.id}
                    data-testid={`documents-table-row-${row.id}`}
                    className="border-b border-border last:border-b-0 hover:bg-bg-muted"
                  >
                    {row.getVisibleCells().map((cell) => (
                      <td
                        key={cell.id}
                        className="whitespace-nowrap px-3 py-2.5 align-middle text-sm text-fg"
                      >
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            )}
          </table>
        </div>
      )}
    </section>
  );
}

// --------------------------------------------------------------------
// Виртуализированное тело таблицы (@tanstack/react-virtual).
// --------------------------------------------------------------------

interface VirtualizedBodyProps {
  rows: Row<ContractSummary>[];
  columns: number;
  headerGroups: ReturnType<ReturnType<typeof useReactTable<ContractSummary>>['getHeaderGroups']>;
}

function VirtualizedBody({ rows, columns, headerGroups }: VirtualizedBodyProps): JSX.Element {
  const parentRef = useRef<HTMLDivElement>(null);
  // Fixed ROW_HEIGHT_PX → estimateSize стабильна, measureElement не нужен
  // (был бы обязателен только для переменной высоты).
  const rowVirtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: useCallback(() => parentRef.current, []),
    estimateSize: useCallback(() => ROW_HEIGHT_PX, []),
    overscan: 10,
  });

  const virtualItems = rowVirtualizer.getVirtualItems();
  const totalSize = rowVirtualizer.getTotalSize();

  // В jsdom getBoundingClientRect() = 0 → virtualItems всегда пустой.
  // Fallback: рендерим весь список подряд, теряя виртуализацию в тестах.
  const items = virtualItems.length > 0 ? virtualItems : null;

  return (
    <div
      ref={parentRef}
      className="max-h-[640px] overflow-auto"
      data-testid="documents-table-virtualized"
      role="region"
      aria-label="Виртуализированный список договоров"
    >
      {/* role="grid" + aria-rowcount/aria-rowindex сохраняют навигацию AT
          при абсолютном позиционировании строк. Каждая строка имеет
          stable aria-rowindex, заголовок = 1, первая строка данных = 2. */}
      <table
        className="w-full border-collapse text-sm text-fg"
        role="grid"
        aria-rowcount={rows.length + 1}
        aria-colcount={columns}
      >
        <thead className="sticky top-0 z-10 bg-bg-muted">
          {headerGroups.map((hg) => (
            <tr key={hg.id} role="row" aria-rowindex={1} className="border-b border-border">
              {hg.headers.map((header, colIdx) => (
                <th
                  key={header.id}
                  scope="col"
                  role="columnheader"
                  aria-colindex={colIdx + 1}
                  className="h-10 whitespace-nowrap px-3 text-left align-middle text-xs font-medium uppercase tracking-wide text-fg-muted"
                >
                  {header.isPlaceholder
                    ? null
                    : flexRender(header.column.columnDef.header, header.getContext())}
                </th>
              ))}
            </tr>
          ))}
        </thead>
        <tbody style={{ height: items ? totalSize : 'auto', position: 'relative' }}>
          {items
            ? items.map((v) => {
                const row = rows[v.index];
                if (!row) return null;
                return (
                  <tr
                    key={row.id}
                    role="row"
                    aria-rowindex={v.index + 2}
                    data-testid={`documents-table-row-${row.id}`}
                    data-index={v.index}
                    style={{
                      position: 'absolute',
                      top: 0,
                      left: 0,
                      width: '100%',
                      transform: `translateY(${v.start}px)`,
                      height: `${ROW_HEIGHT_PX}px`,
                    }}
                    className="border-b border-border hover:bg-bg-muted"
                  >
                    {row.getVisibleCells().map((cell, colIdx) => (
                      <td
                        key={cell.id}
                        role="gridcell"
                        aria-colindex={colIdx + 1}
                        className="whitespace-nowrap px-3 py-2.5 align-middle text-sm text-fg"
                      >
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    ))}
                  </tr>
                );
              })
            : rows.map((row, idx) => (
                <tr
                  key={row.id}
                  role="row"
                  aria-rowindex={idx + 2}
                  data-testid={`documents-table-row-${row.id}`}
                  className="border-b border-border hover:bg-bg-muted"
                  style={{ height: `${ROW_HEIGHT_PX}px` }}
                >
                  {row.getVisibleCells().map((cell, colIdx) => (
                    <td
                      key={cell.id}
                      role="gridcell"
                      aria-colindex={colIdx + 1}
                      className="whitespace-nowrap px-3 py-2.5 align-middle text-sm text-fg"
                    >
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </td>
                  ))}
                </tr>
              ))}
        </tbody>
      </table>
      <p className="sr-only" data-testid="documents-table-rows-total">
        Показано строк: {rows.length}
      </p>
      <p className="sr-only" data-testid="documents-table-columns-total">
        Колонок: {columns}
      </p>
    </div>
  );
}
