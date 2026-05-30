// @vitest-environment jsdom
import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { BusinessSummary } from './BusinessSummary';

afterEach(cleanup);

describe('BusinessSummary', () => {
  it('показывает total в «проверено», прочее — «—»', () => {
    render(<BusinessSummary total={12} />);
    const region = screen.getByRole('region', { name: 'Сводка' });
    expect(within(region).getByText('12')).toBeDefined();
    expect(within(region).getAllByText('—')).toHaveLength(2);
  });

  it('error → role=alert', () => {
    render(<BusinessSummary error={new Error('x')} />);
    expect(screen.getByRole('alert')).toBeDefined();
  });
});
