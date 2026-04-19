// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { TabsFilters } from './tabs-filters';

afterEach(cleanup);

describe('TabsFilters', () => {
  it('рендерит 4 таба с правильным aria-selected', () => {
    render(<TabsFilters value="all" onChange={() => {}} />);
    const tabs = screen.getAllByRole('tab');
    expect(tabs).toHaveLength(4);
    expect(screen.getByTestId('tabs-filters-all').getAttribute('aria-selected')).toBe('true');
    expect(screen.getByTestId('tabs-filters-textual').getAttribute('aria-selected')).toBe('false');
  });

  it('клик по табу вызывает onChange с новым значением', () => {
    const onChange = vi.fn();
    render(<TabsFilters value="all" onChange={onChange} />);
    fireEvent.click(screen.getByTestId('tabs-filters-textual'));
    expect(onChange).toHaveBeenCalledWith('textual');
  });

  it('arrow-навигация переключает табы по WAI-ARIA', () => {
    const onChange = vi.fn();
    render(<TabsFilters value="all" onChange={onChange} />);
    const allTab = screen.getByTestId('tabs-filters-all');
    fireEvent.keyDown(allTab, { key: 'ArrowRight' });
    expect(onChange).toHaveBeenCalledWith('textual');
  });

  it('показывает счётчики, если переданы', () => {
    render(
      <TabsFilters
        value="all"
        onChange={() => {}}
        counters={{ all: 12, textual: 7, structural: 5, 'high-risk': 2 }}
      />,
    );
    expect(screen.getByTestId('tabs-filters-all').textContent).toContain('12');
    expect(screen.getByTestId('tabs-filters-high-risk').textContent).toContain('2');
  });
});
