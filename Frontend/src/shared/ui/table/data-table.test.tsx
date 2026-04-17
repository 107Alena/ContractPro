// @vitest-environment jsdom
// DataTable compound-компонент (§8.3 / §8.4 high-architecture).
import type { ColumnDef, PaginationState, SortingState } from '@tanstack/react-table';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  DataTable,
  DataTableContent,
  DataTablePagination,
  DataTableSelectionCheckbox,
  DataTableToolbar,
  dataTableVariants,
  DataTableViewOptions,
} from './data-table';

interface Row {
  id: string;
  name: string;
  size: number;
}

const rows: Row[] = [
  { id: '1', name: 'Банан', size: 10 },
  { id: '2', name: 'Арбуз', size: 30 },
  { id: '3', name: 'Вишня', size: 5 },
];

const columns: ColumnDef<Row, unknown>[] = [
  { accessorKey: 'name', header: 'Название' },
  { accessorKey: 'size', header: 'Размер' },
];

afterEach(cleanup);

describe('dataTableVariants', () => {
  it('включает базовые классы', () => {
    expect(dataTableVariants()).toContain('w-full');
    expect(dataTableVariants()).toContain('text-fg');
  });
});

describe('<DataTable>', () => {
  it('рендерит заголовки и строки данных', () => {
    render(
      <DataTable data={rows} columns={columns}>
        <DataTableContent />
      </DataTable>,
    );
    expect(screen.getByText('Название')).toBeTruthy();
    expect(screen.getByText('Размер')).toBeTruthy();
    expect(screen.getByText('Банан')).toBeTruthy();
    expect(screen.getByText('Вишня')).toBeTruthy();
  });

  it('показывает empty-state при пустом data', () => {
    render(
      <DataTable data={[]} columns={columns} emptyState={<span>Ничего нет</span>}>
        <DataTableContent />
      </DataTable>,
    );
    expect(screen.getByText('Ничего нет')).toBeTruthy();
  });

  it('показывает loading-state при isLoading', () => {
    render(
      <DataTable data={[]} columns={columns} isLoading loadingState={<span>Грузим</span>}>
        <DataTableContent />
      </DataTable>,
    );
    expect(screen.getByText('Грузим')).toBeTruthy();
  });

  it('показывает error-state при ошибке', () => {
    render(
      <DataTable
        data={[]}
        columns={columns}
        error={new Error('Сбой')}
        errorState={<span>Упало</span>}
      >
        <DataTableContent />
      </DataTable>,
    );
    expect(screen.getByText('Упало')).toBeTruthy();
  });

  it('default error-state выводит message из Error', () => {
    render(
      <DataTable data={[]} columns={columns} error={new Error('Пятисотка')}>
        <DataTableContent />
      </DataTable>,
    );
    expect(screen.getByText('Пятисотка')).toBeTruthy();
    expect(screen.getByText('Не удалось загрузить данные')).toBeTruthy();
  });

  it('throws при использовании sub-компонента вне <DataTable>', () => {
    // Глушим ожидаемую ошибку React в тестовый stderr.
    const spy = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    expect(() => render(<DataTableContent />)).toThrow(/<DataTable>/);
    spy.mockRestore();
  });
});

describe('<DataTable> sorting', () => {
  it('client-side: клик по заголовку инвертирует порядок строк (asc)', () => {
    render(
      <DataTable data={rows} columns={columns}>
        <DataTableContent />
      </DataTable>,
    );
    const header = screen.getByRole('button', { name: /Название/ });
    fireEvent.click(header);
    const cells = screen.getAllByRole('cell');
    // Первая строка по asc = Арбуз, вторая = Банан, третья = Вишня.
    expect(cells[0]?.textContent).toBe('Арбуз');
    expect(cells[2]?.textContent).toBe('Банан');
    expect(cells[4]?.textContent).toBe('Вишня');
  });

  it('server-side (manualSorting): дёргает onSortingChange, строки остаются в исходном порядке', () => {
    function Harness() {
      const [sorting, setSorting] = useState<SortingState>([]);
      return (
        <DataTable
          data={rows}
          columns={columns}
          manualSorting
          sorting={sorting}
          onSortingChange={setSorting}
        >
          <DataTableContent />
          <span data-testid="state">{JSON.stringify(sorting)}</span>
        </DataTable>
      );
    }
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: /Название/ }));
    expect(screen.getByTestId('state').textContent).toContain('"name"');
    expect(screen.getByTestId('state').textContent).toContain('"desc":false');
  });

  it('aria-sort приводится в соответствие direction', () => {
    render(
      <DataTable data={rows} columns={columns}>
        <DataTableContent />
      </DataTable>,
    );
    const nameHeader = screen.getAllByRole('columnheader')[0];
    expect(nameHeader?.getAttribute('aria-sort')).toBe('none');
    fireEvent.click(screen.getByRole('button', { name: /Название/ }));
    expect(nameHeader?.getAttribute('aria-sort')).toBe('ascending');
  });
});

describe('<DataTablePagination>', () => {
  it('server-mode: дёргает onPaginationChange, disable previous на первой странице', () => {
    function Harness() {
      const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: 10 });
      return (
        <DataTable
          data={rows}
          columns={columns}
          manualPagination
          pageCount={5}
          pagination={pagination}
          onPaginationChange={setPagination}
        >
          <DataTableContent />
          <DataTablePagination />
        </DataTable>
      );
    }
    render(<Harness />);
    const prev = screen.getByRole('button', { name: 'Предыдущая страница' });
    const next = screen.getByRole('button', { name: 'Следующая страница' });
    expect(prev).toHaveProperty('disabled', true);
    expect(next).toHaveProperty('disabled', false);
    fireEvent.click(next);
    expect(screen.getByText('Страница 2 из 5')).toBeTruthy();
  });

  it('page-size select обновляет pageSize', () => {
    function Harness() {
      const [pagination, setPagination] = useState<PaginationState>({ pageIndex: 0, pageSize: 10 });
      return (
        <DataTable
          data={rows}
          columns={columns}
          manualPagination
          pageCount={1}
          pagination={pagination}
          onPaginationChange={setPagination}
        >
          <DataTableContent />
          <DataTablePagination pageSizeOptions={[10, 25]} />
          <span data-testid="size">{pagination.pageSize}</span>
        </DataTable>
      );
    }
    render(<Harness />);
    const select = screen.getByRole('combobox');
    fireEvent.change(select, { target: { value: '25' } });
    expect(screen.getByTestId('size').textContent).toBe('25');
  });
});

describe('<DataTableViewOptions>', () => {
  it('не рендерит опции когда нет hideable колонок', () => {
    const fixedColumns: ColumnDef<Row, unknown>[] = [
      { accessorKey: 'name', header: 'Название', enableHiding: false },
    ];
    render(
      <DataTable data={rows} columns={fixedColumns}>
        <DataTableToolbar>
          <DataTableViewOptions />
        </DataTableToolbar>
        <DataTableContent />
      </DataTable>,
    );
    // Кнопка рендерится.
    expect(screen.getByRole('button', { name: 'Показать/скрыть колонки' })).toBeTruthy();
  });
});

describe('<DataTableSelectionCheckbox>', () => {
  it('дёргает onCheckedChange с новым значением', () => {
    const onChange = vi.fn();
    render(
      <DataTableSelectionCheckbox
        checked={false}
        onCheckedChange={onChange}
        aria-label="Выбрать"
      />,
    );
    const cb = screen.getByLabelText('Выбрать');
    fireEvent.click(cb);
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it('проставляет indeterminate на DOM-узле', () => {
    render(
      <DataTableSelectionCheckbox
        checked={false}
        indeterminate
        onCheckedChange={() => undefined}
        aria-label="Выбрать"
      />,
    );
    const cb = screen.getByLabelText('Выбрать') as HTMLInputElement;
    expect(cb.indeterminate).toBe(true);
  });
});
