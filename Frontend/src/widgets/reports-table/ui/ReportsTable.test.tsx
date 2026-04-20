// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { ContractSummary } from '@/entities/contract';

import { ReportsTable } from './ReportsTable';

const sample: ContractSummary[] = [
  {
    contract_id: 'c1',
    title: 'Договор услуг',
    status: 'ACTIVE',
    current_version_number: 2,
    processing_status: 'READY',
    updated_at: '2026-04-19T10:00:00Z',
  },
  {
    contract_id: 'c2',
    title: 'NDA',
    status: 'ACTIVE',
    current_version_number: 1,
    processing_status: 'READY',
    updated_at: '2026-04-18T10:00:00Z',
  },
];

function renderTable(props: Partial<React.ComponentProps<typeof ReportsTable>> = {}): void {
  render(
    <MemoryRouter>
      <ReportsTable items={sample} {...props} />
    </MemoryRouter>,
  );
}

afterEach(cleanup);

describe('ReportsTable', () => {
  it('Populated — рендерит строки и ссылки на договоры', () => {
    renderTable();
    expect(screen.getByText('Договор услуг')).toBeInTheDocument();
    expect(screen.getByText('NDA')).toBeInTheDocument();
    expect(screen.getByTestId('reports-table-title-c1')).toHaveAttribute('href', '/contracts/c1');
  });

  it('Loading — показывает loading body', () => {
    renderTable({ items: [], isLoading: true });
    expect(screen.getByTestId('reports-table-loading')).toBeInTheDocument();
  });

  it('Empty — показывает default empty', () => {
    renderTable({ items: [] });
    expect(screen.getByTestId('reports-table-empty')).toBeInTheDocument();
  });

  it('FilteredEmpty — показывает default filtered empty', () => {
    renderTable({ items: [], hasActiveFilters: true });
    expect(screen.getByTestId('reports-table-empty-filtered')).toBeInTheDocument();
  });

  it('Error — показывает error body + retry', () => {
    const onRetry = vi.fn();
    renderTable({ items: [], error: new Error('boom'), onRetry });
    expect(screen.getByTestId('reports-table-error')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('reports-table-retry'));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it('Row select — клик по строке вызывает onSelectRow', () => {
    const onSelectRow = vi.fn();
    renderTable({ onSelectRow });
    const row = screen.getByTestId('reports-table-row-c1');
    fireEvent.click(row);
    expect(onSelectRow).toHaveBeenCalledWith(expect.objectContaining({ contract_id: 'c1' }));
  });

  it('Row select — клавиша Enter срабатывает как клик', () => {
    const onSelectRow = vi.fn();
    renderTable({ onSelectRow });
    const row = screen.getByTestId('reports-table-row-c2');
    fireEvent.keyDown(row, { key: 'Enter' });
    expect(onSelectRow).toHaveBeenCalledWith(expect.objectContaining({ contract_id: 'c2' }));
  });

  it('Selected row — aria-selected=true + aria-current=true', () => {
    renderTable({ selectedId: 'c1', onSelectRow: vi.fn() });
    const row = screen.getByTestId('reports-table-row-c1');
    expect(row.getAttribute('aria-selected')).toBe('true');
    expect(row.getAttribute('aria-current')).toBe('true');
  });
});
