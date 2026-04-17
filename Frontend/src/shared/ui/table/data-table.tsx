import {
  type Cell,
  type ColumnDef,
  type ColumnFiltersState,
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  type Header,
  type OnChangeFn,
  type PaginationState,
  type Row,
  type RowSelectionState,
  type SortDirection,
  type SortingState,
  type Table,
  type TableOptions,
  useReactTable,
  type VisibilityState,
} from '@tanstack/react-table';
import { cva } from 'class-variance-authority';
import {
  type ChangeEvent,
  createContext,
  type HTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type TableHTMLAttributes,
  useContext,
  useEffect,
  useMemo,
  useRef,
} from 'react';

import { cn } from '@/shared/lib/cn';
import { Button } from '@/shared/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from '@/shared/ui/popover';

// --------------------------------------------------------------------
// Контекст compound-компонента
// --------------------------------------------------------------------

interface DataTableContextValue<T> {
  table: Table<T>;
  isLoading: boolean;
  hasError: boolean;
  emptyState: ReactNode;
  loadingState: ReactNode;
  errorState: ReactNode;
  columnsCount: number;
}

const DataTableContext = createContext<DataTableContextValue<unknown> | null>(null);

function useDataTableContext<T>(): DataTableContextValue<T> {
  const ctx = useContext(DataTableContext);
  if (!ctx) {
    throw new Error('DataTable compound-компоненты должны рендериться внутри <DataTable>.');
  }
  return ctx as DataTableContextValue<T>;
}

/** Хук для низкоуровневого доступа к TanStack `Table`-инстансу изнутри custom-toolbar или pagination. */
export function useDataTable<T>(): Table<T> {
  return useDataTableContext<T>().table;
}

// --------------------------------------------------------------------
// Root-компонент
// --------------------------------------------------------------------

export interface DataTableProps<T> {
  data: T[];
  columns: ColumnDef<T, unknown>[];

  // Server-controlled state + callbacks.
  sorting?: SortingState;
  onSortingChange?: OnChangeFn<SortingState>;
  pagination?: PaginationState;
  onPaginationChange?: OnChangeFn<PaginationState>;
  columnFilters?: ColumnFiltersState;
  onColumnFiltersChange?: OnChangeFn<ColumnFiltersState>;
  rowSelection?: RowSelectionState;
  onRowSelectionChange?: OnChangeFn<RowSelectionState>;
  columnVisibility?: VisibilityState;
  onColumnVisibilityChange?: OnChangeFn<VisibilityState>;

  /** Кол-во страниц при server pagination (если не известно — -1). */
  pageCount?: number;
  manualPagination?: boolean;
  manualSorting?: boolean;
  manualFiltering?: boolean;

  /** Стабильный id строки — критично для row-selection между страницами при manualPagination. */
  getRowId?: TableOptions<T>['getRowId'];
  enableRowSelection?: TableOptions<T>['enableRowSelection'];

  // Slots/state.
  isLoading?: boolean;
  error?: unknown;
  emptyState?: ReactNode;
  loadingState?: ReactNode;
  errorState?: ReactNode;

  className?: string;
  children: ReactNode;
}

export function DataTable<T>({
  data,
  columns,
  sorting,
  onSortingChange,
  pagination,
  onPaginationChange,
  columnFilters,
  onColumnFiltersChange,
  rowSelection,
  onRowSelectionChange,
  columnVisibility,
  onColumnVisibilityChange,
  pageCount,
  manualPagination = false,
  manualSorting = false,
  manualFiltering = false,
  getRowId,
  enableRowSelection,
  isLoading = false,
  error,
  emptyState,
  loadingState,
  errorState,
  className,
  children,
}: DataTableProps<T>) {
  // Собираем state только из переданных слайсов — TanStack обращается с undefined как "не контролируется".
  // Если ни один slice не контролируется снаружи — не передаём state вовсе, иначе таблица считает
  // себя controlled и не держит внутреннее состояние (клик по сортировке в uncontrolled-режиме ломается).
  const state = useMemo(() => {
    const s: Partial<{
      sorting: SortingState;
      pagination: PaginationState;
      columnFilters: ColumnFiltersState;
      rowSelection: RowSelectionState;
      columnVisibility: VisibilityState;
    }> = {};
    if (sorting !== undefined) s.sorting = sorting;
    if (pagination !== undefined) s.pagination = pagination;
    if (columnFilters !== undefined) s.columnFilters = columnFilters;
    if (rowSelection !== undefined) s.rowSelection = rowSelection;
    if (columnVisibility !== undefined) s.columnVisibility = columnVisibility;
    return Object.keys(s).length === 0 ? undefined : s;
  }, [sorting, pagination, columnFilters, rowSelection, columnVisibility]);

  const table = useReactTable<T>({
    data,
    columns,
    ...(state ? { state } : {}),
    pageCount: pageCount ?? -1,
    manualPagination,
    manualSorting,
    manualFiltering,
    enableRowSelection: enableRowSelection ?? false,
    ...(getRowId ? { getRowId } : {}),
    ...(onSortingChange ? { onSortingChange } : {}),
    ...(onPaginationChange ? { onPaginationChange } : {}),
    ...(onColumnFiltersChange ? { onColumnFiltersChange } : {}),
    ...(onRowSelectionChange ? { onRowSelectionChange } : {}),
    ...(onColumnVisibilityChange ? { onColumnVisibilityChange } : {}),
    getCoreRowModel: getCoreRowModel(),
    // Включаем client-side row-model только в не-manual режиме — так сервер-контролируемые
    // таблицы не тратят память на внутренние вычисления, а client-таблицы работают из коробки.
    ...(manualSorting ? {} : { getSortedRowModel: getSortedRowModel() }),
    ...(manualFiltering ? {} : { getFilteredRowModel: getFilteredRowModel() }),
    ...(manualPagination ? {} : { getPaginationRowModel: getPaginationRowModel() }),
  });

  const visibleLeafColumns = table.getVisibleLeafColumns().length;

  // Context пересоздаётся на каждый render: мемоизация по identity table-инстанса ломает
  // uncontrolled-сортировку (TanStack обновляет internal state без смены ссылки на table).
  const ctxValue: DataTableContextValue<T> = {
    table,
    isLoading,
    hasError: error != null,
    emptyState: emptyState ?? <DefaultEmptyState />,
    loadingState: loadingState ?? <DefaultLoadingState />,
    errorState: errorState ?? <DefaultErrorState error={error} />,
    columnsCount: visibleLeafColumns,
  };

  return (
    <DataTableContext.Provider value={ctxValue as DataTableContextValue<unknown>}>
      <div className={cn('flex flex-col gap-3', className)}>{children}</div>
    </DataTableContext.Provider>
  );
}

// --------------------------------------------------------------------
// Toolbar + column visibility
// --------------------------------------------------------------------

export function DataTableToolbar({ className, children, ...rest }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn('flex flex-wrap items-center justify-between gap-2', className)} {...rest}>
      {children}
    </div>
  );
}

export interface DataTableViewOptionsProps {
  /** Текст кнопки-триггера. По умолчанию — "Колонки". */
  triggerLabel?: string;
  className?: string;
}

export function DataTableViewOptions({
  triggerLabel = 'Колонки',
  className,
}: DataTableViewOptionsProps) {
  const { table } = useDataTableContext();
  const toggleable = table.getAllLeafColumns().filter((column) => column.getCanHide());

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant="secondary"
          size="sm"
          className={className}
          aria-label="Показать/скрыть колонки"
        >
          {triggerLabel}
        </Button>
      </PopoverTrigger>
      <PopoverContent size="sm" align="end">
        <div className="flex flex-col gap-2" role="group" aria-label="Видимость колонок">
          {toggleable.length === 0 ? (
            <p className="text-xs text-fg-muted">Нет настраиваемых колонок</p>
          ) : (
            toggleable.map((column) => {
              const headerText =
                typeof column.columnDef.header === 'string' ? column.columnDef.header : column.id;
              return (
                <label
                  key={column.id}
                  className="flex cursor-pointer items-center gap-2 text-sm text-fg"
                >
                  <input
                    type="checkbox"
                    className="h-4 w-4 rounded border-border accent-brand-500"
                    checked={column.getIsVisible()}
                    onChange={(e) => column.toggleVisibility(e.currentTarget.checked)}
                  />
                  {headerText}
                </label>
              );
            })
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

// --------------------------------------------------------------------
// Таблица (thead + tbody + state slots)
// --------------------------------------------------------------------

const tableVariants = cva('w-full border-collapse text-sm text-fg');

export interface DataTableContentProps extends TableHTMLAttributes<HTMLTableElement> {
  /** Плотность строк: comfortable (default) — py-3, compact — py-2. */
  density?: 'comfortable' | 'compact';
}

export function DataTableContent({
  className,
  density = 'comfortable',
  ...rest
}: DataTableContentProps) {
  return (
    <div className="overflow-x-auto rounded-md border border-border">
      <table
        className={cn(tableVariants(), className)}
        data-density={density}
        role="table"
        {...rest}
      >
        <DataTableHead />
        <DataTableBody />
      </table>
    </div>
  );
}

function DataTableHead<T>() {
  const { table } = useDataTableContext<T>();

  return (
    <thead className="bg-bg-muted">
      {table.getHeaderGroups().map((headerGroup) => (
        <tr key={headerGroup.id} className="border-b border-border">
          {headerGroup.headers.map((header) => (
            <DataTableHeaderCell key={header.id} header={header} />
          ))}
        </tr>
      ))}
    </thead>
  );
}

function DataTableHeaderCell<T>({ header }: { header: Header<T, unknown> }) {
  if (header.isPlaceholder) {
    return <th scope="col" className="h-10 px-3" aria-hidden="true" />;
  }

  const canSort = header.column.getCanSort();
  const direction = header.column.getIsSorted() as SortDirection | false;
  const ariaSort: 'ascending' | 'descending' | 'none' | undefined = canSort
    ? direction === 'asc'
      ? 'ascending'
      : direction === 'desc'
        ? 'descending'
        : 'none'
    : undefined;

  return (
    <th
      scope="col"
      aria-sort={ariaSort}
      className="h-10 whitespace-nowrap px-3 text-left align-middle text-xs font-medium uppercase tracking-wide text-fg-muted"
    >
      {canSort ? (
        <button
          type="button"
          onClick={header.column.getToggleSortingHandler()}
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
}

function DataTableBody<T>() {
  const { table, isLoading, hasError, emptyState, loadingState, errorState, columnsCount } =
    useDataTableContext<T>();

  if (isLoading) {
    return (
      <tbody>
        <tr>
          <td colSpan={columnsCount} className="px-3 py-10 text-center">
            {loadingState}
          </td>
        </tr>
      </tbody>
    );
  }

  if (hasError) {
    return (
      <tbody>
        <tr>
          <td colSpan={columnsCount} className="px-3 py-10 text-center">
            {errorState}
          </td>
        </tr>
      </tbody>
    );
  }

  const rows = table.getRowModel().rows;
  if (rows.length === 0) {
    return (
      <tbody>
        <tr>
          <td colSpan={columnsCount} className="px-3 py-10 text-center">
            {emptyState}
          </td>
        </tr>
      </tbody>
    );
  }

  return (
    <tbody>
      {rows.map((row) => (
        <DataTableRow key={row.id} row={row} />
      ))}
    </tbody>
  );
}

function DataTableRow<T>({ row }: { row: Row<T> }) {
  return (
    <tr
      data-state={row.getIsSelected() ? 'selected' : undefined}
      className="border-b border-border last:border-b-0 hover:bg-bg-muted data-[state=selected]:bg-brand-50"
    >
      {row.getVisibleCells().map((cell) => (
        <DataTableCell key={cell.id} cell={cell} />
      ))}
    </tr>
  );
}

function DataTableCell<T>({ cell }: { cell: Cell<T, unknown> }) {
  return (
    <td className="whitespace-nowrap px-3 py-2.5 align-middle text-sm text-fg data-[compact=true]:py-1.5">
      {flexRender(cell.column.columnDef.cell, cell.getContext())}
    </td>
  );
}

// --------------------------------------------------------------------
// Пагинация
// --------------------------------------------------------------------

export interface DataTablePaginationProps {
  /** Варианты размера страницы. По умолчанию [10, 25, 50, 100]. */
  pageSizeOptions?: number[];
  className?: string;
}

export function DataTablePagination({
  pageSizeOptions = [10, 25, 50, 100],
  className,
}: DataTablePaginationProps) {
  const { table } = useDataTableContext();
  const { pageIndex, pageSize } = table.getState().pagination;
  const totalPages = table.getPageCount();
  const hasKnownTotal = totalPages >= 0;
  const rangeLabel = hasKnownTotal
    ? `Страница ${pageIndex + 1} из ${totalPages || 1}`
    : `Страница ${pageIndex + 1}`;

  return (
    <nav
      aria-label="Пагинация таблицы"
      className={cn(
        'flex flex-wrap items-center justify-between gap-3 text-sm text-fg-muted',
        className,
      )}
    >
      <div className="flex items-center gap-2">
        <label className="flex items-center gap-2">
          <span>На странице:</span>
          <select
            value={pageSize}
            onChange={(e) => table.setPageSize(Number(e.target.value))}
            className="h-8 rounded-md border border-border bg-bg px-2 text-sm text-fg focus-visible:outline-none focus-visible:ring"
          >
            {pageSizeOptions.map((size) => (
              <option key={size} value={size}>
                {size}
              </option>
            ))}
          </select>
        </label>
      </div>
      <div className="flex items-center gap-3">
        <span aria-live="polite">{rangeLabel}</span>
        <div className="flex items-center gap-1">
          <Button
            variant="secondary"
            size="sm"
            onClick={() => table.previousPage()}
            disabled={!table.getCanPreviousPage()}
            aria-label="Предыдущая страница"
          >
            ←
          </Button>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => table.nextPage()}
            disabled={!table.getCanNextPage()}
            aria-label="Следующая страница"
          >
            →
          </Button>
        </div>
      </div>
    </nav>
  );
}

// --------------------------------------------------------------------
// Row-selection checkbox helper
// --------------------------------------------------------------------

export interface DataTableSelectionCheckboxProps extends Omit<
  InputHTMLAttributes<HTMLInputElement>,
  'type' | 'checked' | 'onChange'
> {
  checked: boolean;
  indeterminate?: boolean;
  onCheckedChange: (checked: boolean) => void;
}

/**
 * Маленький обёрточный checkbox для selection-колонки: прокидывает `indeterminate`
 * на DOM-узел (HTML-атрибут индетерминантного состояния недоступен декларативно).
 */
export function DataTableSelectionCheckbox({
  checked,
  indeterminate,
  onCheckedChange,
  className,
  ...rest
}: DataTableSelectionCheckboxProps) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = Boolean(indeterminate);
  }, [indeterminate]);
  return (
    <input
      ref={ref}
      type="checkbox"
      checked={checked}
      onChange={(e: ChangeEvent<HTMLInputElement>) => onCheckedChange(e.currentTarget.checked)}
      className={cn('h-4 w-4 rounded border-border accent-brand-500', className)}
      {...rest}
    />
  );
}

// --------------------------------------------------------------------
// Default state components
// --------------------------------------------------------------------

function DefaultEmptyState() {
  return (
    <div className="flex flex-col items-center gap-1 text-fg-muted">
      <p className="text-sm font-medium text-fg">Данных нет</p>
      <p className="text-xs">Попробуйте изменить фильтры или обновить страницу.</p>
    </div>
  );
}

function DefaultLoadingState() {
  return (
    <div className="flex items-center justify-center gap-2 text-sm text-fg-muted">
      <span
        className="inline-block h-4 w-4 animate-spin rounded-full border-2 border-border border-t-brand-500"
        aria-hidden="true"
      />
      <span>Загрузка…</span>
    </div>
  );
}

function DefaultErrorState({ error }: { error: unknown }) {
  const message =
    error instanceof Error
      ? error.message
      : typeof error === 'string'
        ? error
        : 'Неизвестная ошибка';
  return (
    <div className="flex flex-col items-center gap-1 text-sm">
      <p className="font-medium text-danger">Не удалось загрузить данные</p>
      <p className="text-xs text-fg-muted">{message}</p>
    </div>
  );
}

export { tableVariants as dataTableVariants };
