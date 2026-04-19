// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { RiskProfileDelta } from './risk-profile-delta';

afterEach(cleanup);

describe('RiskProfileDelta', () => {
  it('рендерит 3 строки и значения base→target', () => {
    render(
      <RiskProfileDelta
        delta={{ high: -2, medium: 1, low: 0 }}
        baseProfile={{ high: 3, medium: 2, low: 5 }}
        targetProfile={{ high: 1, medium: 3, low: 5 }}
      />,
    );
    expect(screen.getByTestId('risk-profile-delta')).toBeTruthy();
    expect(screen.getByTestId('risk-delta-row-high')).toBeTruthy();
    expect(screen.getByTestId('risk-delta-row-medium')).toBeTruthy();
    expect(screen.getByTestId('risk-delta-row-low')).toBeTruthy();
    expect(screen.getByTestId('risk-delta-value-high').textContent).toBe('-2');
    expect(screen.getByTestId('risk-delta-value-medium').textContent).toBe('+1');
    expect(screen.getByTestId('risk-delta-value-low').textContent).toBe('0');
  });

  it('обрабатывает отсутствие профилей: base→0, target→0', () => {
    render(<RiskProfileDelta delta={{ high: 0, medium: 0, low: 0 }} />);
    expect(screen.getByTestId('risk-delta-value-high').textContent).toBe('0');
  });
});
