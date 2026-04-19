// @vitest-environment jsdom
//
// RTL-тесты ConfirmDeleteContractModal: домен-обёртка над shared ConfirmDialog
// — проверяем, что русскоязычные тексты на месте, variant=danger применён,
// title договора отображается.
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ConfirmDeleteContractModal } from './ConfirmDeleteContractModal';

afterEach(() => cleanup());

describe('ConfirmDeleteContractModal', () => {
  it('рендерит доменные тексты и danger-кнопку «Удалить»', () => {
    render(
      <ConfirmDeleteContractModal
        open
        onOpenChange={() => undefined}
        onConfirm={() => undefined}
        contractTitle="Договор поставки №42"
      />,
    );

    expect(screen.getByText('Удалить договор?')).toBeInTheDocument();
    expect(
      screen.getByText('Договор будет удалён. Его можно будет восстановить из архива.'),
    ).toBeInTheDocument();
    expect(screen.getByText('Договор поставки №42')).toBeInTheDocument();

    const confirmBtn = screen.getByRole('button', { name: 'Удалить' });
    expect(confirmBtn.className).toContain('bg-danger');
  });

  it('confirm click → onConfirm', () => {
    const onConfirm = vi.fn();
    render(
      <ConfirmDeleteContractModal open onOpenChange={() => undefined} onConfirm={onConfirm} />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Удалить' }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it('cancel click → onOpenChange(false)', () => {
    const onOpenChange = vi.fn();
    render(
      <ConfirmDeleteContractModal open onOpenChange={onOpenChange} onConfirm={() => undefined} />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Отмена' }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it('isPending → кнопка «Удалить» aria-busy', () => {
    render(
      <ConfirmDeleteContractModal
        open
        onOpenChange={() => undefined}
        onConfirm={() => undefined}
        isPending
      />,
    );
    expect(screen.getByRole('button', { name: 'Удалить' })).toHaveAttribute('aria-busy', 'true');
  });

  it('contractTitle не указан → блок «Название» скрыт', () => {
    render(
      <ConfirmDeleteContractModal
        open
        onOpenChange={() => undefined}
        onConfirm={() => undefined}
      />,
    );
    expect(screen.queryByText('Название:')).toBeNull();
  });
});
