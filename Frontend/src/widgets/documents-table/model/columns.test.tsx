// @vitest-environment jsdom
//
// Колонки Тип/Риск (FE-TASK-058): реальные данные из ContractSummary, «—» только
// при null. Тестируется через DocumentsTable (колонкам нужен table-context).
import { cleanup, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';

import type { ContractSummary } from '@/entities/contract';

import { DocumentsTable } from '../ui/DocumentsTable';

function renderTable(items: ContractSummary[]): void {
  render(
    <MemoryRouter>
      <DocumentsTable items={items} />
    </MemoryRouter>,
  );
}

const analyzed: ContractSummary = {
  contract_id: 'analyzed',
  title: 'Готовый договор',
  status: 'ACTIVE',
  current_version_number: 2,
  processing_status: 'READY',
  contract_type: 'WORK_CONTRACT',
  risk_level: 'high',
  risk_counts: { high: 3, medium: 1, low: 0 },
  created_at: '2026-04-12T11:00:00Z',
  updated_at: '2026-04-19T16:05:00Z',
};

const pending: ContractSummary = {
  contract_id: 'pending',
  title: 'Непроанализированный',
  status: 'ACTIVE',
  current_version_number: 1,
  processing_status: 'ANALYZING',
  contract_type: null,
  risk_level: null,
  risk_counts: null,
  created_at: '2026-04-17T09:10:00Z',
  updated_at: '2026-04-17T09:10:00Z',
};

afterEach(cleanup);

describe('DocumentsTable columns — Тип/Риск', () => {
  it('Тип — RU-лейбл из contract_type', () => {
    renderTable([analyzed]);
    const row = screen.getByTestId('documents-table-row-analyzed');
    expect(within(row).getByText('Подряд')).toBeInTheDocument();
  });

  it('Риск — RiskBadge по risk_level', () => {
    renderTable([analyzed]);
    const row = screen.getByTestId('documents-table-row-analyzed');
    const badge = within(row).getByTestId('risk-badge');
    expect(badge).toHaveAttribute('data-level', 'high');
    expect(badge).toHaveTextContent('Высокий риск');
  });

  it('null contract_type/risk_level → «—» (не выдумываем)', () => {
    renderTable([pending]);
    const row = screen.getByTestId('documents-table-row-pending');
    // RiskBadge отсутствует, есть «—».
    expect(within(row).queryByTestId('risk-badge')).toBeNull();
    expect(within(row).getAllByText('—').length).toBeGreaterThanOrEqual(2); // Тип + Риск
  });
});
