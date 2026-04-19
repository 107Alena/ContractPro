// ConfirmDeleteContractModal — доменная обёртка над shared ConfirmDialog
// с русскоязычными текстами и danger-variant для destructive-действия.
//
// Модалка — purely presentational; мутация живёт в `useDeleteContract` и
// подключается на уровне page/widget (где есть доступ к title договора и
// навигация после успеха).
import { type ReactNode } from 'react';

import { ConfirmDialog } from '@/shared/ui';

export interface ConfirmDeleteContractModalProps {
  open: boolean;
  onOpenChange: (next: boolean) => void;
  /** Отображаемое название договора (для подтверждения «что именно удаляем»). */
  contractTitle?: ReactNode;
  /** Флаг in-flight мутации — блокирует confirm и показывает спиннер. */
  isPending?: boolean;
  onConfirm: () => void;
}

export function ConfirmDeleteContractModal({
  open,
  onOpenChange,
  contractTitle,
  isPending = false,
  onConfirm,
}: ConfirmDeleteContractModalProps): JSX.Element {
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      onConfirm={onConfirm}
      title="Удалить договор?"
      description="Договор будет удалён. Его можно будет восстановить из архива."
      confirmLabel="Удалить"
      cancelLabel="Отмена"
      variant="danger"
      isPending={isPending}
    >
      {contractTitle !== undefined && (
        <p className="text-sm text-fg">
          <span className="text-fg-muted">Название: </span>
          <strong>{contractTitle}</strong>
        </p>
      )}
    </ConfirmDialog>
  );
}
