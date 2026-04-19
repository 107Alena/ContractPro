// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { PaginationControls } from './PaginationControls';

afterEach(() => cleanup());

describe('PaginationControls', () => {
  it('показывает диапазон «A–B из N»', () => {
    render(<PaginationControls page={2} size={20} total={100} onPageChange={vi.fn()} />);
    expect(screen.getByText(/21–40 из 100/)).toBeTruthy();
  });

  it('total=0 → «Записей нет», кнопок нет', () => {
    const { container } = render(
      <PaginationControls page={1} size={20} total={0} onPageChange={vi.fn()} />,
    );
    expect(screen.getByText('Записей нет')).toBeTruthy();
    expect(container.querySelectorAll('nav button').length).toBe(0);
  });

  it('isLoading → skeleton, кнопки disabled', () => {
    render(<PaginationControls page={1} size={20} total={100} onPageChange={vi.fn()} isLoading />);
    const buttons = screen.getAllByRole('button');
    expect(buttons.every((b) => b.hasAttribute('disabled'))).toBe(true);
  });

  it('onPageChange передаётся в Pagination', () => {
    const onPage = vi.fn();
    render(<PaginationControls page={2} size={20} total={100} onPageChange={onPage} />);
    fireEvent.click(screen.getByRole('button', { name: 'Назад' }));
    expect(onPage).toHaveBeenCalledWith(1);
  });

  it('onSizeChange передаётся в PageSizeSelect', () => {
    const onSize = vi.fn();
    render(
      <PaginationControls
        page={1}
        size={20}
        total={100}
        onPageChange={vi.fn()}
        onSizeChange={onSize}
      />,
    );
    fireEvent.change(screen.getByRole('combobox'), { target: { value: '50' } });
    expect(onSize).toHaveBeenCalledWith(50);
  });

  it('без onSizeChange PageSizeSelect скрыт', () => {
    render(<PaginationControls page={1} size={20} total={100} onPageChange={vi.fn()} />);
    expect(screen.queryByRole('combobox')).toBeNull();
  });

  it('клэмпит page > totalPages (рассинхрон URL и total)', () => {
    render(<PaginationControls page={10} size={20} total={80} onPageChange={vi.fn()} />);
    // 80 записей / 20 size = 4 страницы; page=10 должен быть заклампен на 4.
    // Подпись: «61–80 из 80», не «181–80 из 80».
    expect(screen.getByText(/61–80 из 80/)).toBeTruthy();
  });

  it('isFetching блокирует пагинацию, но не подписи', () => {
    render(<PaginationControls page={2} size={20} total={100} onPageChange={vi.fn()} isFetching />);
    const buttons = screen.getAllByRole('button');
    expect(buttons.every((b) => b.hasAttribute('disabled'))).toBe(true);
    expect(screen.getByText(/21–40 из 100/)).toBeTruthy();
  });
});
