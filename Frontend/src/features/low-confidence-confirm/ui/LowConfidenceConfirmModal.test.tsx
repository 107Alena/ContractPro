// @vitest-environment jsdom
//
// RTL-тесты модалки. confirm-хук инжектится через DI prop, чтобы избежать
// настоящего useMutation/QueryClient — фокус на UI-контракте: рендер
// suggested+alternatives, выбор radio, disable кнопки до выбора, dismiss
// через ESC/кнопку, loading-state.
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { TypeConfirmationEvent } from '../model/types';
import { LowConfidenceConfirmModal } from './LowConfidenceConfirmModal';

function makeEvent(overrides: Partial<TypeConfirmationEvent> = {}): TypeConfirmationEvent {
  return {
    document_id: 'doc-1',
    version_id: 'ver-1',
    status: 'AWAITING_USER_INPUT',
    suggested_type: 'услуги',
    confidence: 0.62,
    threshold: 0.75,
    alternatives: [
      { contract_type: 'подряд', confidence: 0.21 },
      { contract_type: 'NDA', confidence: 0.1 },
    ],
    ...overrides,
  };
}

function makeConfirmStub(
  overrides: Partial<{ confirm: ReturnType<typeof vi.fn>; isPending: boolean }> = {},
): { confirm: ReturnType<typeof vi.fn>; isPending: boolean } {
  return {
    confirm: vi.fn(),
    isPending: false,
    ...overrides,
  };
}

afterEach(() => cleanup());

describe('<LowConfidenceConfirmModal>', () => {
  it('event=null → ничего не рендерит', () => {
    const { container } = render(
      <LowConfidenceConfirmModal event={null} onDismiss={vi.fn()} confirm={makeConfirmStub()} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it('рендерит title, confidence/threshold в %, suggested + alternatives в radio-группе', () => {
    render(
      <LowConfidenceConfirmModal
        event={makeEvent()}
        onDismiss={vi.fn()}
        confirm={makeConfirmStub()}
      />,
    );

    expect(screen.getByText('Уточните тип договора')).toBeTruthy();
    // Описание содержит и confidence (62%) и threshold (75%) одной строкой.
    expect(screen.getByText(/Уверенность модели\s+62\s?%\s+ниже порога\s+75\s?%/)).toBeTruthy();
    expect(screen.getByLabelText(/услуги/i)).toBeTruthy();
    expect(screen.getByLabelText(/подряд/i)).toBeTruthy();
    expect(screen.getByLabelText(/NDA/i)).toBeTruthy();
  });

  it('suggested_type предвыбран по умолчанию', () => {
    render(
      <LowConfidenceConfirmModal
        event={makeEvent()}
        onDismiss={vi.fn()}
        confirm={makeConfirmStub()}
      />,
    );
    const suggested = screen.getByLabelText(/услуги/i) as HTMLInputElement;
    expect(suggested.checked).toBe(true);
  });

  it('клик «Подтвердить» вызывает confirm с выбранным типом', () => {
    const confirmStub = makeConfirmStub();
    render(
      <LowConfidenceConfirmModal event={makeEvent()} onDismiss={vi.fn()} confirm={confirmStub} />,
    );

    fireEvent.click(screen.getByLabelText(/подряд/i));
    fireEvent.click(screen.getByRole('button', { name: 'Подтвердить' }));

    expect(confirmStub.confirm).toHaveBeenCalledWith('подряд');
  });

  it('клик «Отмена» вызывает onDismiss', () => {
    const onDismiss = vi.fn();
    render(
      <LowConfidenceConfirmModal
        event={makeEvent()}
        onDismiss={onDismiss}
        confirm={makeConfirmStub()}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Отмена' }));
    expect(onDismiss).toHaveBeenCalled();
  });

  it('isPending=true → обе кнопки disabled', () => {
    render(
      <LowConfidenceConfirmModal
        event={makeEvent()}
        onDismiss={vi.fn()}
        confirm={makeConfirmStub({ isPending: true })}
      />,
    );
    const confirmBtn = screen.getByRole('button', { name: 'Подтвердить' });
    const cancelBtn = screen.getByRole('button', { name: 'Отмена' });
    expect((confirmBtn as HTMLButtonElement).disabled).toBe(true);
    expect((cancelBtn as HTMLButtonElement).disabled).toBe(true);
  });

  it('alternatives без дубля suggested_type — рендерим уникальные варианты', () => {
    render(
      <LowConfidenceConfirmModal
        event={makeEvent({
          suggested_type: 'услуги',
          alternatives: [
            { contract_type: 'услуги', confidence: 0.4 },
            { contract_type: 'NDA', confidence: 0.2 },
          ],
        })}
        onDismiss={vi.fn()}
        confirm={makeConfirmStub()}
      />,
    );
    // Ровно одно «услуги» (suggested) + одно «NDA».
    expect(screen.getAllByLabelText(/услуги/i)).toHaveLength(1);
    expect(screen.getAllByLabelText(/NDA/i)).toHaveLength(1);
  });

  it('alternatives отсутствует — показывается только suggested', () => {
    render(
      <LowConfidenceConfirmModal
        event={makeEvent({ alternatives: undefined as never })}
        onDismiss={vi.fn()}
        confirm={makeConfirmStub()}
      />,
    );
    const radios = screen.getAllByRole('radio');
    expect(radios).toHaveLength(1);
  });
});
