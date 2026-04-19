// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { ComparisonVerdictCard } from './comparison-verdict-card';

afterEach(cleanup);

describe('ComparisonVerdictCard', () => {
  it('рендерит badge "Лучше" для verdict=better и сравнение high+medium', () => {
    render(
      <ComparisonVerdictCard
        verdict="better"
        baseProfile={{ high: 3, medium: 2, low: 1 }}
        targetProfile={{ high: 1, medium: 2, low: 1 }}
      />,
    );
    expect(screen.getByTestId('comparison-verdict-card')).toBeTruthy();
    expect(screen.getByTestId('comparison-verdict-badge').textContent).toBe('Лучше');
    const summary = screen.getByTestId('comparison-verdict-summary');
    // 3+2=5 → 1+2=3
    expect(summary.textContent).toContain('5');
    expect(summary.textContent).toContain('3');
  });

  it('рендерит "Без изменений" и не показывает summary, если профили не переданы', () => {
    render(<ComparisonVerdictCard verdict="unchanged" />);
    expect(screen.getByTestId('comparison-verdict-badge').textContent).toBe('Без изменений');
    expect(screen.queryByTestId('comparison-verdict-summary')).toBeNull();
  });

  it('рендерит "Смешанные изменения" для verdict=mixed', () => {
    render(<ComparisonVerdictCard verdict="mixed" />);
    expect(screen.getByTestId('comparison-verdict-badge').textContent).toBe('Смешанные изменения');
  });
});
