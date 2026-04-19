// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { ChangeCounters } from './change-counters';

afterEach(cleanup);

describe('ChangeCounters', () => {
  it('рендерит 4 плашки и breakdown-строку', () => {
    render(
      <ChangeCounters
        counters={{
          total: 10,
          added: 3,
          removed: 2,
          modified: 4,
          moved: 1,
          textual: 7,
          structural: 3,
        }}
      />,
    );
    expect(screen.getByTestId('change-counters')).toBeTruthy();
    expect(screen.getByTestId('counter-total').textContent).toContain('10');
    expect(screen.getByTestId('counter-added').textContent).toContain('3');
    expect(screen.getByTestId('counter-removed').textContent).toContain('2');
    // modified + moved = 5
    expect(screen.getByTestId('counter-modified').textContent).toContain('5');
    const breakdown = screen.getByTestId('counter-breakdown');
    expect(breakdown.textContent).toContain('7');
    expect(breakdown.textContent).toContain('3');
  });
});
