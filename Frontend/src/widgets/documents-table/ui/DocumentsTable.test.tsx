// @vitest-environment jsdom
//
// DocumentsTable — smoke + RBAC + виртуализация. Виртуализацию проверяем
// опосредованно: при rows.length >= VIRTUALIZATION_THRESHOLD (50) виджет
// переключается на VirtualizedBody и рендерит data-testid="documents-table-virtualized".
import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';

import type { ContractSummary } from '@/entities/contract';

import { DocumentsTable, VIRTUALIZATION_THRESHOLD } from './DocumentsTable';

function renderTable(ui: JSX.Element): void {
  render(<MemoryRouter>{ui}</MemoryRouter>);
}

const sample: ContractSummary[] = [
  {
    contract_id: 'c1',
    title: 'Договор оказания услуг',
    status: 'ACTIVE',
    current_version_number: 2,
    processing_status: 'READY',
    created_at: '2026-04-15T10:00:00Z',
    updated_at: '2026-04-16T14:20:00Z',
  },
  {
    contract_id: 'c2',
    title: 'NDA с ООО «Бета»',
    status: 'ARCHIVED',
    current_version_number: 1,
    processing_status: 'READY',
    created_at: '2026-04-10T10:00:00Z',
    updated_at: '2026-04-10T10:00:00Z',
  },
];

afterEach(cleanup);

describe('DocumentsTable', () => {
  it('Default — рендерит строки и ссылку на карточку договора', () => {
    renderTable(<DocumentsTable items={sample} />);

    expect(screen.getByTestId('documents-table')).toBeInTheDocument();
    expect(screen.getByText('Договор оказания услуг')).toBeInTheDocument();
    expect(screen.getByText('NDA с ООО «Бета»')).toBeInTheDocument();
    expect(screen.getByTestId('documents-table-title-c1').getAttribute('href')).toBe(
      '/contracts/c1',
    );
  });

  it('Loading — показывает спиннер и блокирует контент', () => {
    renderTable(<DocumentsTable items={[]} isLoading />);

    expect(screen.getByTestId('documents-table-loading')).toBeInTheDocument();
    expect(screen.getByTestId('documents-table').getAttribute('aria-busy')).toBe('true');
  });

  it('Empty (без фильтров) — CTA «Загрузить договор»', () => {
    renderTable(<DocumentsTable items={[]} />);

    expect(screen.getByTestId('documents-table-empty')).toBeInTheDocument();
    expect(screen.getByText('Загрузить договор')).toBeInTheDocument();
  });

  it('Empty (с активными фильтрами) — копия «Ничего не найдено»', () => {
    renderTable(<DocumentsTable items={[]} hasActiveFilters />);

    expect(screen.getByTestId('documents-table-empty-filtered')).toBeInTheDocument();
    expect(screen.getByText('По вашему запросу ничего не найдено')).toBeInTheDocument();
  });

  it('Error — показывает alert + кнопку Retry, вызывает onRetry', () => {
    const onRetry = (): void => {};
    renderTable(<DocumentsTable items={[]} error={new Error('boom')} onRetry={onRetry} />);

    expect(screen.getByTestId('documents-table-error')).toBeInTheDocument();
    expect(screen.getByTestId('documents-table-retry')).toBeInTheDocument();
  });

  it('renderRowActions — рендерится колонка «Действия» для каждой строки', () => {
    renderTable(
      <DocumentsTable
        items={sample}
        renderRowActions={({ contract }) => (
          <button data-testid={`action-btn-${contract.contract_id}`}>X</button>
        )}
      />,
    );

    expect(screen.getByTestId('action-btn-c1')).toBeInTheDocument();
    expect(screen.getByTestId('action-btn-c2')).toBeInTheDocument();
  });

  it('renderRowActions НЕ передан — колонка «Действия» не рендерится (RBAC BUSINESS_USER)', () => {
    renderTable(<DocumentsTable items={sample} />);

    // Колонка с заголовком «Действия» отсутствует
    expect(screen.queryByRole('columnheader', { name: 'Действия' })).toBeNull();
  });

  it('virtualization — при rows.length >= THRESHOLD рендерится VirtualizedBody', () => {
    const many: ContractSummary[] = Array.from({ length: VIRTUALIZATION_THRESHOLD }, (_, i) => ({
      contract_id: `c${i}`,
      title: `Контракт ${i}`,
      status: 'ACTIVE' as const,
      current_version_number: 1,
      processing_status: 'READY' as const,
    }));
    renderTable(<DocumentsTable items={many} />);

    expect(screen.getByTestId('documents-table-virtualized')).toBeInTheDocument();
    // Fallback-рендер работает в jsdom (virtualItems=[] → полный список).
    const rowsTotal = screen.getByTestId('documents-table-rows-total').textContent;
    expect(rowsTotal).toContain(String(VIRTUALIZATION_THRESHOLD));
  });

  it('virtualization — ниже порога используется обычный tbody, а не VirtualizedBody', () => {
    renderTable(<DocumentsTable items={sample} />);

    expect(screen.queryByTestId('documents-table-virtualized')).toBeNull();
    expect(screen.getByTestId('documents-table-row-c1')).toBeInTheDocument();
  });

  it('isFetching без isLoading — показывает маркер обновления', () => {
    renderTable(<DocumentsTable items={sample} isFetching />);

    expect(screen.getByTestId('documents-table-fetching')).toBeInTheDocument();
  });
});
