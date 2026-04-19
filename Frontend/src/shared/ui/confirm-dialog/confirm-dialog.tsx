// ConfirmDialog — generic presentational диалог подтверждения действия.
//
// Используется для destructive-действий: delete, archive, revert. Тонкая
// обёртка над Modal с фиксированной структурой (title + description +
// confirm/cancel). Полностью controlled через `open`/`onOpenChange`, чтобы
// потребитель (feature/page) владел стейтом и мог показать `isPending`.
//
// По умолчанию dismissOnOverlay=false — защита от случайного закрытия
// во время destructive-действия. ESC остаётся включённым (a11y).
import { type ReactNode } from 'react';

import { Button } from '@/shared/ui/button';
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalDescription,
  ModalFooter,
  ModalHeader,
  ModalTitle,
} from '@/shared/ui/modal';

export type ConfirmDialogVariant = 'danger' | 'primary';

export interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (next: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  /** Доп. содержимое тела — например, название удаляемого объекта. */
  children?: ReactNode;
  /** Текст кнопки подтверждения. По умолчанию «Подтвердить». */
  confirmLabel?: ReactNode;
  /** Текст кнопки отмены. По умолчанию «Отмена». */
  cancelLabel?: ReactNode;
  /** Визуал confirm-кнопки: danger (красная) для delete, primary (синяя) по умолч. */
  variant?: ConfirmDialogVariant;
  /** Отображает спиннер и блокирует confirm. Cancel остаётся активным. */
  isPending?: boolean;
  /** Блокирует клик по overlay для закрытия. По умолчанию true (destructive). */
  disableOverlayClose?: boolean;
  /** Вызывается при нажатии на confirm-кнопку. */
  onConfirm: () => void;
}

export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  confirmLabel = 'Подтвердить',
  cancelLabel = 'Отмена',
  variant = 'primary',
  isPending = false,
  disableOverlayClose = true,
  onConfirm,
}: ConfirmDialogProps): JSX.Element {
  return (
    <Modal open={open} onOpenChange={onOpenChange}>
      <ModalContent size="sm" dismissOnOverlay={!disableOverlayClose}>
        <ModalHeader>
          <ModalTitle>{title}</ModalTitle>
          {description !== undefined && <ModalDescription>{description}</ModalDescription>}
        </ModalHeader>
        {children !== undefined && <ModalBody>{children}</ModalBody>}
        <ModalFooter>
          <Button
            type="button"
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={isPending}
          >
            {cancelLabel}
          </Button>
          <Button
            type="button"
            variant={variant === 'danger' ? 'danger' : 'primary'}
            onClick={onConfirm}
            loading={isPending}
          >
            {confirmLabel}
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
