// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import type { ContractSummary } from '@/entities/contract';

import { computeStripCounters, ContractsMetricsStrip } from './ContractsMetricsStrip';

afterEach(cleanup);

const s = (status: NonNullable<ContractSummary['processing_status']>): ContractSummary => ({
  contract_id: status,
  processing_status: status,
});

describe('computeStripCounters', () => {
  it('считает in_progress и attention по processing_status', () => {
    const c = computeStripCounters([
      s('ANALYZING'),
      s('PROCESSING'),
      s('AWAITING_USER_INPUT'),
      s('FAILED'),
      s('READY'),
    ]);
    expect(c.inProgress).toBe(2); // ANALYZING + PROCESSING
    expect(c.attention).toBe(2); // AWAITING + FAILED
  });

  it('пустой ввод → нули', () => {
    expect(computeStripCounters([])).toEqual({ inProgress: 0, attention: 0 });
  });
});

describe('ContractsMetricsStrip', () => {
  it('Default — total реальный, «высокий риск» = «—», без «завершено сегодня»', () => {
    render(<ContractsMetricsStrip items={[s('ANALYZING'), s('FAILED')]} total={42} />);
    expect(screen.getByTestId('contracts-metrics-strip')).toBeInTheDocument();
    expect(screen.getByTestId('contracts-metrics-strip-card-total')).toHaveTextContent('42');
    expect(screen.getByTestId('contracts-metrics-strip-card-in-progress')).toHaveTextContent('1');
    expect(screen.getByTestId('contracts-metrics-strip-card-attention')).toHaveTextContent('1');
    expect(screen.getByTestId('contracts-metrics-strip-card-high-risk')).toHaveTextContent('—');
    expect(screen.queryByTestId('contracts-metrics-strip-card-completed-today')).toBeNull();
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
    render(<ContractsMetricsStrip items={[s('READY'), s('READY')]} />);
    expect(screen.getByTestId('contracts-metrics-strip-card-total')).toHaveTextContent('2');
  });
});
