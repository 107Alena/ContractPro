// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { FilterDefinition, FilterGroupValue } from '../model/types';
import { MoreFiltersModal } from './MoreFiltersModal';

afterEach(() => cleanup());

const DEFS: readonly FilterDefinition[] = [
  {
    key: 'type',
    label: 'Тип договора',
    kind: 'multi',
    options: [
      { value: 'SUPPLY', label: 'Поставка' },
      { value: 'SERVICE', label: 'Услуги' },
    ],
  },
];

function emptyValues(): FilterGroupValue {
  return { type: [] };
}

describe('MoreFiltersModal', () => {
  it('не рендерится при open=false', () => {
    render(
      <MoreFiltersModal
        open={false}
        onOpenChange={vi.fn()}
        definitions={DEFS}
        values={emptyValues()}
        onToggleOption={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('рендерит заголовок и опции при open=true', () => {
    render(
      <MoreFiltersModal
        open
        onOpenChange={vi.fn()}
        definitions={DEFS}
        values={emptyValues()}
        onToggleOption={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    expect(screen.getByRole('dialog')).toBeTruthy();
    expect(screen.getByText('Тип договора')).toBeTruthy();
    expect(screen.getByTestId('more-filter-type-SUPPLY')).toBeTruthy();
  });

  it('клик по опции вызывает onToggleOption', () => {
    const onToggle = vi.fn();
    render(
      <MoreFiltersModal
        open
        onOpenChange={vi.fn()}
        definitions={DEFS}
        values={emptyValues()}
        onToggleOption={onToggle}
        onClear={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId('more-filter-type-SUPPLY'));
    expect(onToggle).toHaveBeenCalledWith('type', 'SUPPLY');
  });

  it('«Сбросить всё» вызывает onClear()', () => {
    const onClear = vi.fn();
    render(
      <MoreFiltersModal
        open
        onOpenChange={vi.fn()}
        definitions={DEFS}
        values={emptyValues()}
        onToggleOption={vi.fn()}
        onClear={onClear}
      />,
    );
    fireEvent.click(screen.getByTestId('more-filter-clear'));
    expect(onClear).toHaveBeenCalledWith();
  });

  it('клик по «Готово» закрывает модалку через onOpenChange', () => {
    const onOpenChange = vi.fn();
    render(
      <MoreFiltersModal
        open
        onOpenChange={onOpenChange}
        definitions={DEFS}
        values={emptyValues()}
        onToggleOption={vi.fn()}
        onClear={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Готово' }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
