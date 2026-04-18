// LowConfidenceConfirmModal — presentational-диалог подтверждения типа договора
// (FR-2.1.3).
//
// Контракт UI:
// - Title: «Уточните тип договора»
// - Description: «Уверенность 62 % ниже порога 75 %»
// - Группа radio: suggested_type + alternatives[].contract_type. v1: только
//   предопределённые варианты. Custom-input — TODO (см. FR-2.1.3, требует
//   server-side whitelist валидации, на frontend в v1 не делаем).
// - Кнопки: «Отмена» (dismiss), «Подтвердить» (disabled до выбора).
// - dismissOnOverlay по умолчанию true; ESC закрывает (модалка неблокирующая —
//   backend поставит watchdog FAILED по таймауту 24h).
//
// Поведение мутации (`useConfirmType`) живёт в Provider'е выше: модалка
// принимает уже готовый объект `confirm` (или его проекцию) — это держит её
// purely presentational и тривиальной для тестирования без QueryClient.
//
// Native <input type="radio"> вместо @radix-ui/react-radio-group — экономия
// зависимости (radix-radio-group не установлен), доступность достигается
// label+for + group через fieldset+legend (a11y-эквивалент).
import { useEffect, useId, useState } from 'react';

import {
  Button,
  Modal,
  ModalBody,
  ModalContent,
  ModalDescription,
  ModalFooter,
  ModalHeader,
  ModalTitle,
} from '@/shared/ui';

import type { TypeAlternative, TypeConfirmationEvent } from '../model/types';

const formatPercent = (value: number): string =>
  // value в долях (0..1). Locale ru → «62 %».
  new Intl.NumberFormat('ru-RU', {
    style: 'percent',
    maximumFractionDigits: 0,
  }).format(value);

interface ChoiceProps {
  id: string;
  name: string;
  value: string;
  checked: boolean;
  confidence?: number;
  isSuggested?: boolean;
  onSelect: (value: string) => void;
}

function ModalChoice({
  id,
  name,
  value,
  checked,
  confidence,
  isSuggested,
  onSelect,
}: ChoiceProps): JSX.Element {
  return (
    <label
      htmlFor={id}
      className="flex cursor-pointer items-start gap-3 rounded-md border border-border px-3 py-2 hover:bg-bg-muted has-[input:checked]:border-fg has-[input:checked]:bg-bg-muted"
    >
      <input
        id={id}
        type="radio"
        name={name}
        value={value}
        checked={checked}
        onChange={() => onSelect(value)}
        className="mt-1 h-4 w-4 cursor-pointer accent-brand-500 focus-visible:ring focus-visible:ring-offset-0"
      />
      <span className="flex-1">
        <span className="block text-sm font-medium text-fg">
          {value}
          {isSuggested && (
            <span className="ml-2 rounded-sm bg-bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-fg-muted">
              рекомендовано
            </span>
          )}
        </span>
        {confidence !== undefined && (
          <span className="block text-xs text-fg-muted">
            Уверенность: {formatPercent(confidence)}
          </span>
        )}
      </span>
    </label>
  );
}

/** Минимальная проекция useConfirmType, нужная модалке. */
export interface ConfirmHandle {
  confirm: (contractType: string) => void;
  isPending: boolean;
}

export interface LowConfidenceConfirmModalProps {
  /** Активный SSE-event от backend. Когда null — модалка не рендерится. */
  event: TypeConfirmationEvent | null;
  /** Закрытие без подтверждения (ESC, кнопка «Отмена», клик по overlay). */
  onDismiss: () => void;
  /** Объект мутации из `useConfirmType()`. Provider создаёт её один раз и
   *  передаёт сюда — модалка остаётся чисто презентационной. */
  confirm: ConfirmHandle;
}

export function LowConfidenceConfirmModal({
  event,
  onDismiss,
  confirm,
}: LowConfidenceConfirmModalProps): JSX.Element | null {
  const groupId = useId();
  const [selected, setSelected] = useState<string | null>(event?.suggested_type ?? null);

  // Сброс выбора при смене event'а (например, прилетел новый
  // type_confirmation_required для другой версии).
  useEffect(() => {
    setSelected(event?.suggested_type ?? null);
  }, [event?.version_id, event?.suggested_type]);

  if (!event) return null;

  const choices: TypeAlternative[] = [
    { contract_type: event.suggested_type, confidence: event.confidence },
    ...(event.alternatives ?? []).filter((alt) => alt.contract_type !== event.suggested_type),
  ];

  const handleConfirm = (): void => {
    if (!selected) return;
    confirm.confirm(selected);
  };

  const isPending = confirm.isPending;

  return (
    <Modal
      open
      onOpenChange={(open) => {
        if (!open) onDismiss();
      }}
    >
      <ModalContent size="md" aria-labelledby={`${groupId}-title`}>
        <ModalHeader>
          <ModalTitle id={`${groupId}-title`}>Уточните тип договора</ModalTitle>
          <ModalDescription>
            Уверенность модели {formatPercent(event.confidence)} ниже порога{' '}
            {formatPercent(event.threshold)}. Выберите тип договора, чтобы продолжить анализ.
          </ModalDescription>
        </ModalHeader>
        <ModalBody>
          <fieldset className="flex flex-col gap-2 border-0 p-0">
            <legend className="sr-only">Тип договора</legend>
            {choices.map((choice, index) => (
              <ModalChoice
                key={choice.contract_type}
                id={`${groupId}-choice-${index}`}
                name={`${groupId}-contract-type`}
                value={choice.contract_type}
                checked={selected === choice.contract_type}
                confidence={choice.confidence}
                isSuggested={index === 0}
                onSelect={setSelected}
              />
            ))}
          </fieldset>
        </ModalBody>
        <ModalFooter>
          <Button variant="ghost" onClick={onDismiss} disabled={isPending}>
            Отмена
          </Button>
          <Button
            variant="primary"
            onClick={handleConfirm}
            disabled={!selected || isPending}
            loading={isPending}
          >
            Подтвердить
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
