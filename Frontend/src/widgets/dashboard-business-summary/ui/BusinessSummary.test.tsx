// @vitest-environment jsdom
import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { BusinessSummary } from './BusinessSummary';

afterEach(cleanup);

describe('BusinessSummary', () => {
  it('показывает total в «проверено» и inProgress в «в работе»', () => {
    render(<BusinessSummary total={12} inProgress={3} />);
    const region = screen.getByRole('article', { name: 'Сводка' });
    expect(within(region).getByText('12')).toBeDefined();
    expect(within(region).getByText('3')).toBeDefined();
    expect(within(region).getByText('проверено')).toBeDefined();
    expect(within(region).getByText('в работе')).toBeDefined();
    // нет плейсхолдеров — оба значения реальны
    expect(within(region).queryByText('—')).toBeNull();
  });

  it('inProgress=0 — показывает 0, а не «—»', () => {
    render(<BusinessSummary total={5} inProgress={0} />);
    const region = screen.getByRole('article', { name: 'Сводка' });
    expect(within(region).getByText('0')).toBeDefined();
    expect(within(region).queryByText('—')).toBeNull();
  });

  it('inProgress отсутствует (stats недоступны) → «в работе» = «—»', () => {
    render(<BusinessSummary total={12} />);
    const region = screen.getByRole('article', { name: 'Сводка' });
    expect(within(region).getByText('12')).toBeDefined();
    // только один плейсхолдер «—» («в работе»)
    expect(within(region).getAllByText('—')).toHaveLength(1);
  });

  it('error → role=alert', () => {
    render(<BusinessSummary error={new Error('x')} />);
    expect(screen.getByRole('alert')).toBeDefined();
  });
});
