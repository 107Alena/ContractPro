// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { FilterDefinition, FilterGroupValue } from '../model/types';
import { FilterChips } from './FilterChips';

afterEach(() => cleanup());

const DEFS: readonly FilterDefinition[] = [
  {
    key: 'status',
    label: 'Статус',
    kind: 'single',
    options: [
      { value: 'ACTIVE', label: 'Активные' },
      { value: 'ARCHIVED', label: 'В архиве' },
    ],
  },
  {
    key: 'type',
    label: 'Тип',
    kind: 'multi',
    options: [
      { value: 'SUPPLY', label: 'Поставка' },
      { value: 'SERVICE', label: 'Услуги' },
    ],
    pinned: false,
  },
];

function emptyValues(): FilterGroupValue {
  return { status: '', type: [] };
}

describe('FilterChips', () => {
  it('рендерит чипы для pinned definitions', () => {
    render(
      <FilterChips
        definitions={DEFS}
        values={emptyValues()}
        onToggleOption={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    expect(screen.getByTestId('filter-chip-status-ACTIVE')).toBeTruthy();
    expect(screen.getByTestId('filter-chip-status-ARCHIVED')).toBeTruthy();
    expect(screen.queryByTestId('filter-chip-type-SUPPLY')).toBeNull();
  });

  it('клик по чипу вызывает onToggleOption', () => {
    const onToggle = vi.fn();
    render(
      <FilterChips
        definitions={DEFS}
        values={emptyValues()}
        onToggleOption={onToggle}
        onClear={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId('filter-chip-status-ACTIVE'));
    expect(onToggle).toHaveBeenCalledWith('status', 'ACTIVE');
  });

  it('кнопка «Ещё фильтры» есть при наличии non-pinned def', () => {
    render(
      <FilterChips
        definitions={DEFS}
        values={emptyValues()}
        onToggleOption={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    expect(screen.getByTestId('filter-chips-more')).toBeTruthy();
  });

  it('кнопка «Сбросить» появляется только при активных фильтрах', () => {
    const { rerender } = render(
      <FilterChips
        definitions={DEFS}
        values={emptyValues()}
        onToggleOption={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    expect(screen.queryByTestId('filter-chips-clear')).toBeNull();
    rerender(
      <FilterChips
        definitions={DEFS}
        values={{ status: 'ACTIVE', type: [] }}
        onToggleOption={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    expect(screen.getByTestId('filter-chips-clear')).toBeTruthy();
  });

  it('«Сбросить» вызывает onClear() без аргумента', () => {
    const onClear = vi.fn();
    render(
      <FilterChips
        definitions={DEFS}
        values={{ status: 'ACTIVE', type: [] }}
        onToggleOption={vi.fn()}
        onClear={onClear}
      />,
    );
    fireEvent.click(screen.getByTestId('filter-chips-clear'));
    expect(onClear).toHaveBeenCalledWith();
  });

  it('активные фильтры показываются в списке active-tokens', () => {
    render(
      <FilterChips
        definitions={DEFS}
        values={{ status: 'ACTIVE', type: ['SUPPLY'] }}
        onToggleOption={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    const activeList = screen.getByLabelText('Активные фильтры');
    expect(activeList.querySelectorAll('[role="listitem"]')).toHaveLength(2);
    expect(screen.getAllByLabelText('Убрать фильтр Активные').length).toBeGreaterThan(0);
    expect(screen.getAllByLabelText('Убрать фильтр Поставка').length).toBeGreaterThan(0);
  });

  it('clicks on «Ещё фильтры» открывают modal', () => {
    render(
      <FilterChips
        definitions={DEFS}
        values={emptyValues()}
        onToggleOption={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId('filter-chips-more'));
    // Модалка из Radix рендерится в Portal
    expect(screen.getByRole('dialog')).toBeTruthy();
  });
});
