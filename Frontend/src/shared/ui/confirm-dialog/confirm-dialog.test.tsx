// @vitest-environment jsdom
//
// RTL-тесты ConfirmDialog. Фокус на контракте: рендер title/description,
// клик по confirm-кнопке вызывает onConfirm, cancel → onOpenChange(false),
// isPending блокирует confirm и показывает спиннер (aria-busy), danger-variant
// применяет danger-стиль кнопки.
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ConfirmDialog } from './confirm-dialog';

afterEach(() => cleanup());

describe('ConfirmDialog', () => {
  it('рендерит title и description', () => {
    render(
      <ConfirmDialog
        open
        onOpenChange={() => undefined}
        onConfirm={() => undefined}
        title="Удалить договор"
        description="Действие необратимо"
      />,
    );
    expect(screen.getByText('Удалить договор')).toBeInTheDocument();
    expect(screen.getByText('Действие необратимо')).toBeInTheDocument();
  });

  it('дефолтные тексты кнопок: «Отмена» и «Подтвердить»', () => {
    render(
      <ConfirmDialog
        open
        onOpenChange={() => undefined}
        onConfirm={() => undefined}
        title="Title"
      />,
    );
    expect(screen.getByRole('button', { name: 'Отмена' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Подтвердить' })).toBeInTheDocument();
  });

  it('confirm click → onConfirm; cancel click → onOpenChange(false)', () => {
    const onConfirm = vi.fn();
    const onOpenChange = vi.fn();
    render(
      <ConfirmDialog
        open
        onOpenChange={onOpenChange}
        onConfirm={onConfirm}
        title="T"
        confirmLabel="Удалить"
        cancelLabel="Отмена"
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Удалить' }));
    expect(onConfirm).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole('button', { name: 'Отмена' }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it('isPending → confirm-кнопка aria-busy и не вызывает onConfirm', () => {
    const onConfirm = vi.fn();
    render(
      <ConfirmDialog
        open
        onOpenChange={() => undefined}
        onConfirm={onConfirm}
        title="T"
        confirmLabel="Удалить"
        isPending
      />,
    );

    const confirmBtn = screen.getByRole('button', { name: 'Удалить' });
    expect(confirmBtn).toHaveAttribute('aria-busy', 'true');
    fireEvent.click(confirmBtn);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('isPending → cancel тоже заблокирован (защита от двойного клика)', () => {
    const onOpenChange = vi.fn();
    render(
      <ConfirmDialog
        open
        onOpenChange={onOpenChange}
        onConfirm={() => undefined}
        title="T"
        isPending
      />,
    );
    const cancelBtn = screen.getByRole('button', { name: 'Отмена' });
    expect(cancelBtn).toBeDisabled();
    fireEvent.click(cancelBtn);
    expect(onOpenChange).not.toHaveBeenCalled();
  });

  it('variant=danger → confirm-кнопка получает danger-стиль (bg-danger)', () => {
    render(
      <ConfirmDialog
        open
        onOpenChange={() => undefined}
        onConfirm={() => undefined}
        title="T"
        confirmLabel="Удалить"
        variant="danger"
      />,
    );
    const btn = screen.getByRole('button', { name: 'Удалить' });
    expect(btn.className).toContain('bg-danger');
  });

  it('open=false → диалог не отрендерен в DOM', () => {
    render(
      <ConfirmDialog
        open={false}
        onOpenChange={() => undefined}
        onConfirm={() => undefined}
        title="Скрытый"
      />,
    );
    expect(screen.queryByText('Скрытый')).toBeNull();
  });

  it('children рендерятся внутри body', () => {
    render(
      <ConfirmDialog open onOpenChange={() => undefined} onConfirm={() => undefined} title="T">
        <p>Название: Договор №42</p>
      </ConfirmDialog>,
    );
    expect(screen.getByText('Название: Договор №42')).toBeInTheDocument();
  });
});
