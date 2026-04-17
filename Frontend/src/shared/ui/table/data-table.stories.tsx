import type { Meta, StoryObj } from '@storybook/react';
import type { ColumnDef, PaginationState, SortingState } from '@tanstack/react-table';
import { useMemo, useState } from 'react';

import { Badge } from '@/shared/ui/badge';

import {
  DataTable,
  DataTableContent,
  DataTablePagination,
  DataTableSelectionCheckbox,
  DataTableToolbar,
  DataTableViewOptions,
} from './data-table';

interface ContractRow {
  id: string;
  title: string;
  status: 'DRAFT' | 'READY' | 'FAILED';
  risk: 'high' | 'medium' | 'low';
  updatedAt: string;
}

const sampleData: ContractRow[] = [
  {
    id: '1',
    title: 'Договор поставки №42',
    status: 'READY',
    risk: 'medium',
    updatedAt: '2026-04-12',
  },
  { id: '2', title: 'NDA ООО «Ромашка»', status: 'DRAFT', risk: 'low', updatedAt: '2026-04-14' },
  {
    id: '3',
    title: 'Аренда помещения ул. Вавилова',
    status: 'READY',
    risk: 'high',
    updatedAt: '2026-04-15',
  },
  {
    id: '4',
    title: 'Лицензионное соглашение ACME',
    status: 'FAILED',
    risk: 'medium',
    updatedAt: '2026-04-16',
  },
  {
    id: '5',
    title: 'Договор оказания услуг',
    status: 'READY',
    risk: 'low',
    updatedAt: '2026-04-17',
  },
];

const statusVariant: Record<ContractRow['status'], 'success' | 'warning' | 'danger'> = {
  READY: 'success',
  DRAFT: 'warning',
  FAILED: 'danger',
};

const statusLabel: Record<ContractRow['status'], string> = {
  READY: 'Готово',
  DRAFT: 'Черновик',
  FAILED: 'Ошибка',
};

const baseColumns: ColumnDef<ContractRow, unknown>[] = [
  { accessorKey: 'title', header: 'Документ' },
  {
    accessorKey: 'status',
    header: 'Статус',
    cell: ({ row }) => (
      <Badge variant={statusVariant[row.original.status]}>{statusLabel[row.original.status]}</Badge>
    ),
    enableSorting: false,
  },
  {
    accessorKey: 'risk',
    header: 'Риск',
    cell: ({ row }) => <Badge variant="neutral">{row.original.risk}</Badge>,
  },
  {
    accessorKey: 'updatedAt',
    header: 'Обновлён',
  },
];

const meta: Meta = {
  title: 'Shared/DataTable',
  parameters: { layout: 'padded' },
  tags: ['autodocs'],
};

export default meta;

type Story = StoryObj;

export const Default: Story = {
  render: () => (
    <DataTable data={sampleData} columns={baseColumns}>
      <DataTableContent />
    </DataTable>
  ),
};

export const Loading: Story = {
  render: () => (
    <DataTable data={[]} columns={baseColumns} isLoading>
      <DataTableContent />
    </DataTable>
  ),
};

export const Empty: Story = {
  render: () => (
    <DataTable data={[]} columns={baseColumns}>
      <DataTableContent />
    </DataTable>
  ),
};

export const ErrorState: Story = {
  name: 'Error',
  render: () => (
    <DataTable
      data={[]}
      columns={baseColumns}
      error={new window.Error('Сервис временно недоступен (502).')}
    >
      <DataTableContent />
    </DataTable>
  ),
};

export const WithSorting: Story = {
  render: () => (
    <DataTable data={sampleData} columns={baseColumns}>
      <DataTableContent />
    </DataTable>
  ),
};

export const WithPagination: Story = {
  name: 'WithPagination (server-mode)',
  render: () => {
    function PaginatedSample() {
      const [pagination, setPagination] = useState<PaginationState>({
        pageIndex: 0,
        pageSize: 2,
      });
      const [sorting, setSorting] = useState<SortingState>([]);
      // Эмулируем серверный ответ: нарезаем sampleData по pagination/sorting.
      const slice = useMemo(() => {
        const sorted = [...sampleData];
        if (sorting[0]) {
          const s = sorting[0];
          sorted.sort((a, b) => {
            const av = a[s.id as keyof ContractRow];
            const bv = b[s.id as keyof ContractRow];
            return (av > bv ? 1 : av < bv ? -1 : 0) * (s.desc ? -1 : 1);
          });
        }
        return sorted.slice(
          pagination.pageIndex * pagination.pageSize,
          (pagination.pageIndex + 1) * pagination.pageSize,
        );
      }, [pagination, sorting]);

      return (
        <DataTable
          data={slice}
          columns={baseColumns}
          manualPagination
          manualSorting
          pageCount={Math.ceil(sampleData.length / pagination.pageSize)}
          pagination={pagination}
          onPaginationChange={setPagination}
          sorting={sorting}
          onSortingChange={setSorting}
        >
          <DataTableContent />
          <DataTablePagination pageSizeOptions={[2, 5, 10]} />
        </DataTable>
      );
    }
    return <PaginatedSample />;
  },
};

export const WithRowSelection: Story = {
  render: () => {
    function SelectableSample() {
      const [rowSelection, setRowSelection] = useState<Record<string, boolean>>({});
      const selectionColumns = useMemo<ColumnDef<ContractRow, unknown>[]>(
        () => [
          {
            id: 'select',
            enableSorting: false,
            enableHiding: false,
            header: ({ table }) => (
              <DataTableSelectionCheckbox
                checked={table.getIsAllRowsSelected()}
                indeterminate={table.getIsSomeRowsSelected()}
                onCheckedChange={(v) => table.toggleAllRowsSelected(v)}
                aria-label="Выбрать все строки"
              />
            ),
            cell: ({ row }) => (
              <DataTableSelectionCheckbox
                checked={row.getIsSelected()}
                onCheckedChange={(v) => row.toggleSelected(v)}
                aria-label={`Выбрать строку ${row.original.title}`}
              />
            ),
          },
          ...baseColumns,
        ],
        [],
      );

      const selectedCount = Object.values(rowSelection).filter(Boolean).length;

      return (
        <DataTable
          data={sampleData}
          columns={selectionColumns}
          enableRowSelection
          rowSelection={rowSelection}
          onRowSelectionChange={setRowSelection}
          getRowId={(row) => row.id}
        >
          <DataTableToolbar>
            <span className="text-sm text-fg-muted">Выбрано: {selectedCount}</span>
          </DataTableToolbar>
          <DataTableContent />
        </DataTable>
      );
    }
    return <SelectableSample />;
  },
};

export const WithColumnVisibility: Story = {
  render: () => (
    <DataTable data={sampleData} columns={baseColumns}>
      <DataTableToolbar>
        <span className="text-sm text-fg-muted">
          Переключение видимости колонок через кнопку справа
        </span>
        <DataTableViewOptions />
      </DataTableToolbar>
      <DataTableContent />
    </DataTable>
  ),
};
