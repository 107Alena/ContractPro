// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { SearchInput } from './search-input';

afterEach(() => cleanup());

describe('SearchInput', () => {
  it('рендерит input с placeholder', () => {
    render(<SearchInput value="" onValueChange={vi.fn()} placeholder="Поиск документов" />);
    const input = screen.getByRole('searchbox') as HTMLInputElement;
    expect(input.placeholder).toBe('Поиск документов');
  });

  it('onValueChange синхронно вызывается при вводе (debounceMs=0)', () => {
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

  it('Escape очищает поле', () => {
    const onChange = vi.fn();
    render(<SearchInput value="foo" onValueChange={onChange} />);
    fireEvent.keyDown(screen.getByRole('searchbox'), { key: 'Escape' });
    expect(onChange).toHaveBeenCalledWith('');
  });

  it('isPending показывает спиннер вместо иконки поиска', () => {
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

  it('debounceMs=300 откладывает onValueChange, onInputChange — синхронно', async () => {
    vi.useFakeTimers();
    try {
      const onChange = vi.fn();
      const onInputChange = vi.fn();
      render(
        <SearchInput
          value=""
          onValueChange={onChange}
          onInputChange={onInputChange}
          debounceMs={300}
        />,
      );
      fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'a' } });
      expect(onInputChange).toHaveBeenCalledWith('a');
      expect(onChange).not.toHaveBeenCalled();
      act(() => {
        vi.advanceTimersByTime(300);
      });
      expect(onChange).toHaveBeenCalledWith('a');
    } finally {
      vi.useRealTimers();
    }
  });

  it('debounce отменяется при Escape и сразу эмитится пустая строка', () => {
    vi.useFakeTimers();
    try {
      const onChange = vi.fn();
      render(<SearchInput value="abc" onValueChange={onChange} debounceMs={300} />);
      fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'abcd' } });
      fireEvent.keyDown(screen.getByRole('searchbox'), { key: 'Escape' });
      expect(onChange).toHaveBeenLastCalledWith('');
      act(() => {
        vi.advanceTimersByTime(500);
      });
      // после Escape 'abcd' не должно прилететь
      expect(onChange).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it('Escape не эмитит ничего если поле уже пустое', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<SearchInput value="" onValueChange={onChange} />);
    const input = screen.getByRole('searchbox');
    input.focus();
    await user.keyboard('{Escape}');
    expect(onChange).not.toHaveBeenCalled();
  });
});
