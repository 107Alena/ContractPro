// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import type { ContractSummary } from '@/entities/contract';

import { computeStripCounters, ContractsMetricsStrip } from './ContractsMetricsStrip';

afterEach(cleanup);

const sample: ContractSummary[] = [
  { contract_id: '1', title: 'a', status: 'ACTIVE' },
  { contract_id: '2', title: 'b', status: 'ACTIVE' },
  { contract_id: '3', title: 'c', status: 'ARCHIVED' },
  { contract_id: '4', title: 'd', status: 'DELETED' },
  { contract_id: '5', title: 'e' },
];

describe('computeStripCounters', () => {
  it('группирует по DocumentStatus', () => {
    const counters = computeStripCounters(sample, 42);
    expect(counters.total).toBe(42);
    expect(counters.active).toBe(2);
    expect(counters.archived).toBe(1);
    expect(counters.deleted).toBe(1);
  });

  it('пустой ввод → нули (кроме total)', () => {
    const counters = computeStripCounters([], 0);
    expect(counters).toEqual({ total: 0, active: 0, archived: 0, deleted: 0 });
  });
});

describe('ContractsMetricsStrip', () => {
  it('Default — рендерит 4 карточки', () => {
    render(<ContractsMetricsStrip items={sample} total={42} />);
    expect(screen.getByTestId('contracts-metrics-strip')).toBeInTheDocument();
    expect(screen.getByTestId('contracts-metrics-strip-card-total')).toHaveTextContent('42');
    expect(screen.getByTestId('contracts-metrics-strip-card-active')).toHaveTextContent('2');
    expect(screen.getByTestId('contracts-metrics-strip-card-archived')).toHaveTextContent('1');
    expect(screen.getByTestId('contracts-metrics-strip-card-deleted')).toHaveTextContent('1');
  });

  it('Loading — спиннер и aria-busy', () => {
    render(<ContractsMetricsStrip isLoading />);
    expect(screen.getByTestId('contracts-metrics-strip-loading')).toBeInTheDocument();
  });

  it('Error — role="alert"', () => {
    render(<ContractsMetricsStrip error={new Error('network')} />);
    expect(screen.getByTestId('contracts-metrics-strip-error')).toBeInTheDocument();
  });

  it('без total → считает из items.length', () => {
    render(<ContractsMetricsStrip items={sample.slice(0, 2)} />);
    expect(screen.getByTestId('contracts-metrics-strip-card-total')).toHaveTextContent('2');
  });
});
