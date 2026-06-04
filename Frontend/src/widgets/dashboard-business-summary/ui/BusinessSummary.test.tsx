// @vitest-environment jsdom
import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { BusinessSummary } from './BusinessSummary';

afterEach(cleanup);

describe('BusinessSummary', () => {
  it('показывает total в «проверено», «в работе» — «—»', () => {
    render(<BusinessSummary total={12} />);
    const region = screen.getByRole('article', { name: 'Сводка' });
    expect(within(region).getByText('12')).toBeDefined();
    expect(within(region).getByText('в работе')).toBeDefined();
    // только один плейсхолдер «—» («в работе»)
    expect(within(region).getAllByText('—')).toHaveLength(1);
  });

  it('error → role=alert', () => {
    render(<BusinessSummary error={new Error('x')} />);
    expect(screen.getByRole('alert')).toBeDefined();
  });
});
