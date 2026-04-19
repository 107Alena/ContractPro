// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { SearchInput } from './SearchInput';

afterEach(() => cleanup());

describe('SearchInput', () => {
  it('рендерит input с placeholder', () => {
    render(<SearchInput value="" onValueChange={vi.fn()} placeholder="Поиск документов" />);
    const input = screen.getByRole('searchbox') as HTMLInputElement;
    expect(input.placeholder).toBe('Поиск документов');
  });

  it('onValueChange вызывается при вводе', () => {
    const onChange = vi.fn();
    render(<SearchInput value="" onValueChange={onChange} />);
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'abc' } });
    expect(onChange).toHaveBeenCalledWith('abc');
  });

  it('кнопка очистки появляется при value.length > 0', () => {
    render(<SearchInput value="hello" onValueChange={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Очистить' })).toBeTruthy();
  });

  it('кнопка очистки вызывает onValueChange("")', () => {
    const onChange = vi.fn();
    render(<SearchInput value="hello" onValueChange={onChange} />);
    fireEvent.click(screen.getByRole('button', { name: 'Очистить' }));
    expect(onChange).toHaveBeenCalledWith('');
  });

  it('кнопка очистки скрыта при clearable=false', () => {
    render(<SearchInput value="hello" onValueChange={vi.fn()} clearable={false} />);
    expect(screen.queryByRole('button', { name: 'Очистить' })).toBeNull();
  });

  it('Escape очищает поле если есть значение', () => {
    const onChange = vi.fn();
    render(<SearchInput value="foo" onValueChange={onChange} />);
    fireEvent.keyDown(screen.getByRole('searchbox'), { key: 'Escape' });
    expect(onChange).toHaveBeenCalledWith('');
  });

  it('isPending показывает спиннер и скрывает иконку поиска', () => {
    const { container } = render(<SearchInput value="" onValueChange={vi.fn()} isPending />);
    expect(
      container.querySelector('[role="status"], [aria-hidden="true"].animate-spin'),
    ).not.toBeNull();
  });

  it('disabled блокирует input и скрывает clear-кнопку', () => {
    render(<SearchInput value="hello" onValueChange={vi.fn()} disabled />);
    expect((screen.getByRole('searchbox') as HTMLInputElement).disabled).toBe(true);
    expect(screen.queryByRole('button', { name: 'Очистить' })).toBeNull();
  });
});
