// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import type { ContractSummary } from '@/entities/contract';

import { computeReportsCounters, ReportsMetrics } from './ReportsMetrics';

const NOW = Date.parse('2026-04-20T12:00:00Z');

const items: ContractSummary[] = [
  {
    contract_id: 'c1',
    title: 'Отчёт 1',
    status: 'ACTIVE',
    processing_status: 'READY',
    created_at: '2026-04-18T10:00:00Z',
    updated_at: '2026-04-19T10:00:00Z',
  },
  {
    contract_id: 'c2',
    title: 'Отчёт 2',
    status: 'ACTIVE',
    processing_status: 'READY',
    created_at: '2026-04-17T10:00:00Z',
    updated_at: '2026-04-18T10:00:00Z',
  },
  {
    contract_id: 'c3',
    title: 'Отчёт 3',
    status: 'ACTIVE',
    processing_status: 'PARTIALLY_FAILED',
    created_at: '2026-03-31T10:00:00Z',
    updated_at: '2026-04-01T10:00:00Z',
  },
];

afterEach(cleanup);

describe('ReportsMetrics', () => {
  it('computeReportsCounters считает 2 READY / 1 PARTIAL / 2 recent (7 days)', () => {
    const c = computeReportsCounters(items, 99, NOW);
    expect(c).toEqual({ total: 99, ready: 2, partial: 1, recent: 2 });
  });

  it('пустые items → нули кроме total', () => {
    const c = computeReportsCounters([], 0, NOW);
    expect(c).toEqual({ total: 0, ready: 0, partial: 0, recent: 0 });
  });

  it('Loading — показывает спиннер', () => {
    render(<ReportsMetrics isLoading />);
    expect(screen.getByTestId('reports-metrics-loading')).toBeInTheDocument();
  });

  it('Populated — 4 карточки + total=42', () => {
    render(<ReportsMetrics items={items} total={42} now={NOW} />);
    expect(screen.getByTestId('reports-metrics')).toBeInTheDocument();
    expect(screen.getByTestId('reports-metrics-card-total')).toHaveTextContent('42');
    expect(screen.getByTestId('reports-metrics-card-ready')).toHaveTextContent('2');
    expect(screen.getByTestId('reports-metrics-card-partial')).toHaveTextContent('1');
    expect(screen.getByTestId('reports-metrics-card-recent')).toHaveTextContent('2');
  });

  it('Error — alert-секция', () => {
    render(<ReportsMetrics error={new Error('network')} />);
    expect(screen.getByTestId('reports-metrics-error')).toBeInTheDocument();
  });
});
