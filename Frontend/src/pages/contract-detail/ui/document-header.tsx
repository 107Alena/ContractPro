// DocumentHeader — шапка карточки договора (экран 8 Figma, §17.4).
// Показывает название, статус документа (ACTIVE/ARCHIVED), статус текущей
// версии и даты. Кнопки действий (архив, удалить) — FE-TASK-040 (features).
//
// Не вынесен в widgets/, потому что специфичен только для ContractDetailPage
// и не переиспользуется — компоновка и набор полей уникальны этой карточке.
import type { ContractDetails } from '@/entities/contract';
import { StatusBadge } from '@/entities/version';
import { Badge } from '@/shared/ui';

export interface DocumentHeaderProps {
  contract: ContractDetails;
}

const DOCUMENT_STATUS_LABEL: Record<'ACTIVE' | 'ARCHIVED' | 'DELETED', string> = {
  ACTIVE: 'Активен',
  ARCHIVED: 'В архиве',
  DELETED: 'Удалён',
};

const DOCUMENT_STATUS_TONE: Record<
  'ACTIVE' | 'ARCHIVED' | 'DELETED',
  'success' | 'warning' | 'danger'
> = {
  ACTIVE: 'success',
  ARCHIVED: 'warning',
  DELETED: 'danger',
};

function formatDate(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleDateString('ru-RU', { day: '2-digit', month: 'long', year: 'numeric' });
}

export function DocumentHeader({ contract }: DocumentHeaderProps): JSX.Element {
  const version = contract.current_version;
  const title = contract.title ?? 'Договор без названия';
  const docStatus = contract.status;

  return (
    <header
      data-testid="document-header"
      className="flex flex-col gap-2 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <div className="flex flex-wrap items-center gap-2">
        <h1 className="text-2xl font-semibold text-fg">{title}</h1>
        {docStatus ? (
          <Badge variant={DOCUMENT_STATUS_TONE[docStatus]}>
            {DOCUMENT_STATUS_LABEL[docStatus]}
          </Badge>
        ) : null}
        {version?.processing_status ? <StatusBadge status={version.processing_status} /> : null}
      </div>
      <dl className="grid grid-cols-[max-content,1fr] gap-x-4 gap-y-1 text-sm text-fg-muted md:grid-cols-[max-content,1fr,max-content,1fr]">
        <dt>Создан:</dt>
        <dd className="text-fg">{formatDate(contract.created_at)}</dd>
        <dt>Обновлён:</dt>
        <dd className="text-fg">{formatDate(contract.updated_at)}</dd>
        {version?.version_number ? (
          <>
            <dt>Текущая версия:</dt>
            <dd className="text-fg">v{version.version_number}</dd>
          </>
        ) : null}
        {version?.source_file_name ? (
          <>
            <dt>Исходный файл:</dt>
            <dd className="text-fg">{version.source_file_name}</dd>
          </>
        ) : null}
      </dl>
    </header>
  );
}
