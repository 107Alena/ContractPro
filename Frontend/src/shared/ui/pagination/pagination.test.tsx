// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { PageSizeSelect, Pagination } from './pagination';

afterEach(() => cleanup());

describe('Pagination', () => {
  it('пустой nav при totalPages <= 1', () => {
    const { container } = render(<Pagination page={1} totalPages={1} onPageChange={vi.fn()} />);
    expect(container.querySelector('nav')).toBeTruthy();
    expect(container.querySelectorAll('button')).toHaveLength(0);
  });

  it('рендерит все страницы если их немного', () => {
    render(<Pagination page={2} totalPages={5} onPageChange={vi.fn()} />);
    for (const n of [1, 2, 3, 4, 5]) {
      expect(screen.getByLabelText(new RegExp(`Страница ${n}(, текущая)?`))).toBeTruthy();
    }
  });

  it('aria-current="page" у текущей страницы', () => {
    render(<Pagination page={3} totalPages={5} onPageChange={vi.fn()} />);
    const current = screen.getByLabelText('Страница 3, текущая');
    expect(current.getAttribute('aria-current')).toBe('page');
  });

  it('клик по странице вызывает onPageChange', () => {
    const onPageChange = vi.fn();
    render(<Pagination page={3} totalPages={5} onPageChange={onPageChange} />);
    fireEvent.click(screen.getByLabelText('Страница 1'));
    expect(onPageChange).toHaveBeenCalledWith(1);
  });

  it('Назад недоступен на первой странице', () => {
    render(<Pagination page={1} totalPages={5} onPageChange={vi.fn()} />);
    const prev = screen.getByRole('button', { name: 'Назад' });
    expect(prev.hasAttribute('disabled')).toBe(true);
  });

  it('Вперёд недоступен на последней странице', () => {
    render(<Pagination page={5} totalPages={5} onPageChange={vi.fn()} />);
    const next = screen.getByRole('button', { name: 'Вперёд' });
    expect(next.hasAttribute('disabled')).toBe(true);
  });

  it('ellipsis отображается при большом количестве страниц', () => {
    render(<Pagination page={50} totalPages={100} onPageChange={vi.fn()} />);
    const ellipses = screen.getAllByText('…');
    expect(ellipses.length).toBeGreaterThanOrEqual(1);
  });

  it('не вызывает onPageChange при клике по текущей странице', () => {
    const onPageChange = vi.fn();
    render(<Pagination page={2} totalPages={5} onPageChange={onPageChange} />);
    fireEvent.click(screen.getByLabelText('Страница 2, текущая'));
    expect(onPageChange).not.toHaveBeenCalled();
  });

  it('disabled блокирует все кнопки', () => {
    render(<Pagination page={2} totalPages={5} onPageChange={vi.fn()} disabled />);
    const buttons = screen.getAllByRole('button');
    for (const b of buttons) expect(b.hasAttribute('disabled')).toBe(true);
  });

  it('showPrevNext=false скрывает кнопки', () => {
    render(<Pagination page={2} totalPages={5} onPageChange={vi.fn()} showPrevNext={false} />);
    expect(screen.queryByRole('button', { name: 'Назад' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Вперёд' })).toBeNull();
  });

  it('клэмпит page в диапазон [1, totalPages]', () => {
    render(<Pagination page={99} totalPages={5} onPageChange={vi.fn()} />);
    expect(screen.getByLabelText('Страница 5, текущая')).toBeTruthy();
  });
});

describe('PageSizeSelect', () => {
  it('рендерит select с опциями', () => {
    render(<PageSizeSelect value={20} options={[10, 20, 50]} onChange={vi.fn()} />);
    expect(screen.getByRole('combobox')).toBeTruthy();
    expect((screen.getByRole('combobox') as HTMLSelectElement).value).toBe('20');
  });

  it('onChange вызывается при выборе значения', () => {
    const onChange = vi.fn();
    render(<PageSizeSelect value={20} options={[10, 20, 50]} onChange={onChange} />);
    fireEvent.change(screen.getByRole('combobox'), { target: { value: '50' } });
    expect(onChange).toHaveBeenCalledWith(50);
  });
});
