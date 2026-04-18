// ExportShareModal — модалка «Экспорт и отправка отчёта» (§17.5, UR-10, FE-TASK-039).
//
// Состав:
//   - Две карточки форматов (PDF, DOCX) × две кнопки на карточку
//     («Скачать» и «Скопировать ссылку»).
//   - Download: useExportDownload → window.location.assign(presigned_url).
//   - Share:    useShareLink → navigator.clipboard.writeText(presigned_url) +
//               checkmark на 1500мс + toast «Ссылка скопирована».
//   - Error:    onError → toast с title/hint из ERROR_UX (§7.3).
// RBAC:
//   - Гейтинг вынесен НАРУЖУ: открывающий компонент (кнопка «Скачать/Поделиться»)
//     сам проверяет useCanExport() (§5.6). Модалка ожидает, что её вообще
//     открывают только для авторизованных ролей.
//   - Defensive rendering: если по какой-то причине модалку открыли без прав —
//     показываем EmptyState вместо карточек (403 на сервере — дополнительный
//     слой защиты).
import type { ReactElement } from 'react';

import { type ExportFormat, useExportDownload } from '@/features/export-download';
import { useShareLink } from '@/features/share-link';
import { useCanExport } from '@/shared/auth/use-can-export';
import { cn } from '@/shared/lib/cn';
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
import { toast } from '@/shared/ui/toast';

interface FormatMeta {
  id: ExportFormat;
  label: string;
  description: string;
}

const FORMATS: readonly FormatMeta[] = [
  {
    id: 'pdf',
    label: 'PDF',
    description: 'Подписанный отчёт для печати и рассылки.',
  },
  {
    id: 'docx',
    label: 'DOCX',
    description: 'Редактируемая версия для юриста или бизнес-партнёра.',
  },
] as const;

export interface ExportShareModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  contractId: string;
  versionId: string;
  /** Заголовок по умолчанию — "Экспорт и отправка отчёта". */
  title?: string;
  /** @internal DI для тестов (вместо window.location.assign). */
  navigate?: (url: string) => void;
  /** className на ModalContent (для Storybook-вариантов). */
  className?: string;
}

interface FormatCardProps {
  format: FormatMeta;
  contractId: string;
  versionId: string;
  navigate?: (url: string) => void;
}

function CheckmarkIcon(): ReactElement {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      className="h-4 w-4"
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.5"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <polyline points="5 11 9 15 15 6" />
    </svg>
  );
}

function FormatCard({ format, contractId, versionId, navigate }: FormatCardProps): ReactElement {
  const exportMutation = useExportDownload({
    ...(navigate && { navigate }),
    onError: (_err, msg) => {
      toast.error({
        title: msg.title,
        ...(msg.hint && { description: msg.hint }),
      });
    },
  });

  const shareMutation = useShareLink({
    onSuccess: (_data, meta) => {
      if (meta.copied) {
        toast.success({ title: 'Ссылка скопирована в буфер обмена' });
      } else {
        toast.warning({
          title: 'Не удалось скопировать ссылку',
          description: 'Скопируйте из адресной строки вручную.',
        });
      }
    },
    onError: (_err, msg) => {
      toast.error({
        title: msg.title,
        ...(msg.hint && { description: msg.hint }),
      });
    },
  });

  const downloadBusy = exportMutation.isPending;
  const shareBusy = shareMutation.isPending;
  const shareJustCopied = shareMutation.copied && !shareBusy;

  return (
    <article
      aria-label={`Формат ${format.label}`}
      className={cn('flex flex-col gap-3 rounded-md border border-border bg-bg p-4', 'shadow-sm')}
    >
      <header className="flex items-baseline justify-between gap-2">
        <h3 className="text-base font-semibold text-fg">{format.label}</h3>
        <span className="text-xs uppercase tracking-wide text-fg-muted">
          {format.id.toUpperCase()}
        </span>
      </header>
      <p className="text-sm text-fg-muted">{format.description}</p>
      <div className="flex flex-col gap-2 sm:flex-row">
        <Button
          variant="primary"
          size="md"
          loading={downloadBusy}
          disabled={shareBusy}
          onClick={() =>
            exportMutation.download({
              contractId,
              versionId,
              format: format.id,
            })
          }
          data-testid={`export-download-${format.id}`}
        >
          Скачать
        </Button>
        <Button
          variant="secondary"
          size="md"
          loading={shareBusy}
          disabled={downloadBusy}
          iconLeft={shareJustCopied ? <CheckmarkIcon /> : undefined}
          onClick={() =>
            shareMutation.share({
              contractId,
              versionId,
              format: format.id,
            })
          }
          data-testid={`export-share-${format.id}`}
        >
          {shareJustCopied ? 'Ссылка скопирована' : 'Скопировать ссылку'}
        </Button>
      </div>
    </article>
  );
}

export function ExportShareModal({
  open,
  onOpenChange,
  contractId,
  versionId,
  title = 'Экспорт и отправка отчёта',
  navigate,
  className,
}: ExportShareModalProps): ReactElement {
  const canExport = useCanExport();

  return (
    <Modal open={open} onOpenChange={onOpenChange}>
      <ModalContent size="lg" className={className} aria-describedby="export-share-description">
        <ModalHeader>
          <ModalTitle>{title}</ModalTitle>
          <ModalDescription id="export-share-description">
            Защищённая ссылка действует 5 минут. Скачайте PDF или DOCX либо отправьте одноразовую
            ссылку коллеге.
          </ModalDescription>
        </ModalHeader>

        <ModalBody>
          {canExport ? (
            <div className="grid gap-3 sm:grid-cols-2">
              {FORMATS.map((format) => (
                <FormatCard
                  key={format.id}
                  format={format}
                  contractId={contractId}
                  versionId={versionId}
                  {...(navigate && { navigate })}
                />
              ))}
            </div>
          ) : (
            <div
              role="note"
              className="rounded-md border border-dashed border-border bg-bg-muted p-4 text-sm text-fg-muted"
            >
              У вас нет прав на экспорт отчётов. Обратитесь к администратору организации.
            </div>
          )}
        </ModalBody>

        <ModalFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Закрыть
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
