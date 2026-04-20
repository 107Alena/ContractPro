// ReportDetailPanel (FE-TASK-048) — боковая панель с метаданными и действиями
// выбранного отчёта на странице «Отчёты» (Figma 9, §17.4).
//
// Собирает инфо из ContractSummary (без отдельного API-вызова в v1). Действия:
//   - «Открыть результаты» → Link на /contracts/:id.
//   - «Экспорт и отправка отчёта» → открывает ExportShareModal (поднимается
//     page-компонентом через onOpenShare). Кнопка экспорта гейтится
//     useCanExport() — Pattern B §5.6.1.
//
// Dialog-контракт использован легковесно (side-panel, а не модалка):
//   `<aside>` даёт неявную complementary-role + aria-labelledby. Focus-trap не
//   нужен — пользователь должен иметь возможность уйти клавиатурой на другую
//   строку таблицы. Закрытие — кнопкой «Закрыть» или Escape.
import { useEffect, useRef } from 'react';
import { Link } from 'react-router-dom';

import { type ContractSummary } from '@/entities/contract';
import { StatusBadge } from '@/entities/version';
import { useCanExport } from '@/shared/auth/use-can-export';
import { Button, buttonVariants } from '@/shared/ui';

function formatDateTime(iso?: string): string {
  if (!iso) return '—';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleString('ru-RU', {
    day: '2-digit',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export interface ReportDetailPanelProps {
  contract: ContractSummary | null;
  onClose: () => void;
  /** Открывает ExportShareModal на уровне page. Вызывается с contractId+versionId. */
  onOpenShare?: (input: { contractId: string; versionId: string }) => void;
}

export function ReportDetailPanel({
  contract,
  onClose,
  onOpenShare,
}: ReportDetailPanelProps): JSX.Element | null {
  const canExport = useCanExport();
  const titleId = 'report-detail-panel-title';
  const closeBtnRef = useRef<HTMLButtonElement>(null);

  // Escape to close. При смене выбранной строки — focus на кнопку закрытия,
  // чтобы скринридер проговорил смену контекста.
  useEffect(() => {
    if (!contract) return;
    closeBtnRef.current?.focus();

    function onKeyDown(e: KeyboardEvent): void {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      }
    }
    window.addEventListener('keydown', onKeyDown);
    return (): void => window.removeEventListener('keydown', onKeyDown);
  }, [contract, onClose]);

  if (!contract) return null;

  const title = contract.title ?? 'Без названия';
  const version = contract.current_version_number;
  const updatedAt = contract.updated_at ?? contract.created_at;
  const canOpenShare =
    canExport && onOpenShare != null && contract.contract_id != null && version != null;

  const handleOpenShare = (): void => {
    if (!canOpenShare) return;
    onOpenShare!({
      contractId: contract.contract_id as string,
      versionId: String(version),
    });
  };

  return (
    <aside
      aria-labelledby={titleId}
      data-testid="report-detail-panel"
      className="flex w-full max-w-sm flex-col gap-4 rounded-md border border-border bg-bg p-4 shadow-md"
    >
      <header className="flex items-start justify-between gap-3">
        <div>
          <h2 id={titleId} className="text-lg font-semibold text-fg">
            {title}
          </h2>
          <p className="mt-1 text-xs uppercase tracking-wide text-fg-muted">Детали отчёта</p>
        </div>
        <Button
          ref={closeBtnRef}
          type="button"
          variant="ghost"
          size="sm"
          onClick={onClose}
          data-testid="report-detail-panel-close"
          aria-label="Закрыть панель деталей"
        >
          Закрыть
        </Button>
      </header>

      <dl className="grid grid-cols-2 gap-3 text-sm">
        <div>
          <dt className="text-xs uppercase tracking-wide text-fg-muted">Версия</dt>
          <dd className="mt-1 text-fg">{version != null ? `v${version}` : '—'}</dd>
        </div>
        <div>
          <dt className="text-xs uppercase tracking-wide text-fg-muted">Обновлён</dt>
          <dd className="mt-1 text-fg">{formatDateTime(updatedAt)}</dd>
        </div>
        <div className="col-span-2">
          <dt className="text-xs uppercase tracking-wide text-fg-muted">Статус обработки</dt>
          <dd className="mt-1">
            <StatusBadge status={contract.processing_status} />
          </dd>
        </div>
      </dl>

      <div className="flex flex-col gap-2">
        {contract.contract_id ? (
          <Link
            to={`/contracts/${encodeURIComponent(contract.contract_id)}`}
            className={buttonVariants({ variant: 'primary', size: 'md' })}
            data-testid="report-detail-panel-open"
          >
            Открыть результаты
          </Link>
        ) : null}
        <Button
          type="button"
          variant="secondary"
          size="md"
          disabled={!canOpenShare}
          onClick={handleOpenShare}
          data-testid="report-detail-panel-share"
        >
          {canExport ? 'Экспорт и отправка отчёта' : 'Экспорт недоступен'}
        </Button>
        {!canExport ? (
          <p className="text-xs text-fg-muted">
            Экспорт и отправка доступны юристам и администраторам организации.
          </p>
        ) : null}
      </div>
    </aside>
  );
}
