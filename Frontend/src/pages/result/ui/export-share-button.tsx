// ExportShareButton — триггер модалки «Экспорт и отправка отчёта» в header
// ResultPage. Гейт `useCanExport()` управляет: (a) полным скрытием для
// ролей без прав, (b) disabled+tooltip для BUSINESS_USER без org-флага.
// Сама модалка (features/export-download + share-link) — FE-TASK-039.
import { useCallback, useState } from 'react';

import { useCanExport } from '@/shared/auth/use-can-export';
import { Button } from '@/shared/ui/button';
import { ExportShareModal } from '@/widgets/export-share-modal';

export interface ExportShareButtonProps {
  contractId: string;
  versionId: string;
  /** Отключает кнопку, пока данные не готовы (PROCESSING/FAILED/REJECTED). */
  disabled?: boolean | undefined;
}

export function ExportShareButton({
  contractId,
  versionId,
  disabled,
}: ExportShareButtonProps): JSX.Element | null {
  const canExport = useCanExport();
  const [open, setOpen] = useState(false);

  const handleOpen = useCallback(() => setOpen(true), []);

  if (!canExport) return null;

  return (
    <>
      <Button
        type="button"
        variant="primary"
        onClick={handleOpen}
        disabled={disabled === true}
        data-testid="export-share-button"
      >
        Скачать отчёт
      </Button>
      <ExportShareModal
        open={open}
        onOpenChange={setOpen}
        contractId={contractId}
        versionId={versionId}
      />
    </>
  );
}
