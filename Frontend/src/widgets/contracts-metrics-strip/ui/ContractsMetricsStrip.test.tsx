// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import type { ContractSummary } from '@/entities/contract';

import { computeStripCounters, ContractsMetricsStrip } from './ContractsMetricsStrip';

afterEach(cleanup);

const s = (
  status: NonNullable<ContractSummary['processing_status']>,
  riskLevel: ContractSummary['risk_level'] = null,
): ContractSummary => ({
  contract_id: `${status}-${riskLevel ?? 'none'}`,
  title: status,
  status: 'ACTIVE',
  processing_status: status,
  risk_level: riskLevel,
  created_at: '2026-04-15T10:00:00Z',
  updated_at: '2026-04-15T10:00:00Z',
});

describe('computeStripCounters', () => {
  it('считает in_progress, attention и highRisk по processing_status/risk_level', () => {
    const c = computeStripCounters([
      s('ANALYZING'),
      s('PROCESSING'),
      s('AWAITING_USER_INPUT'),
      s('FAILED'),
      s('READY', 'high'),
      s('READY', 'low'),
    ]);
    expect(c.inProgress).toBe(2); // ANALYZING + PROCESSING
    expect(c.attention).toBe(2); // AWAITING + FAILED
    expect(c.highRisk).toBe(1); // только READY+high
  });

  it('пустой ввод → нули', () => {
    expect(computeStripCounters([])).toEqual({ inProgress: 0, attention: 0, highRisk: 0 });
  });
});

describe('ContractsMetricsStrip', () => {
  it('Default — total реальный, «высокий риск» из risk_level, без «завершено сегодня»', () => {
    render(
      <ContractsMetricsStrip
        items={[s('ANALYZING'), s('FAILED'), s('READY', 'high')]}
        total={42}
      />,
    );
    expect(screen.getByTestId('contracts-metrics-strip')).toBeInTheDocument();
    expect(screen.getByTestId('contracts-metrics-strip-card-total')).toHaveTextContent('42');
    expect(screen.getByTestId('contracts-metrics-strip-card-in-progress')).toHaveTextContent('1');
    expect(screen.getByTestId('contracts-metrics-strip-card-attention')).toHaveTextContent('1');
    expect(screen.getByTestId('contracts-metrics-strip-card-high-risk')).toHaveTextContent('1');
    expect(screen.queryByTestId('contracts-metrics-strip-card-completed-today')).toBeNull();
  });

  it('видимая отметка охвата «по текущей странице»', () => {
    render(<ContractsMetricsStrip items={[s('READY', 'high')]} total={5} />);
    expect(screen.getByTestId('contracts-metrics-strip-scope-note')).toBeInTheDocument();
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
    render(<ContractsMetricsStrip items={[s('READY', 'low'), s('READY', 'high')]} />);
    expect(screen.getByTestId('contracts-metrics-strip-card-total')).toHaveTextContent('2');
  });
});
