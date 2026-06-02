// DocumentHeader — шапка карточки договора (Figma 306:2 → Page Intro 309:8 +
// Document Meta Card 310:2). Композирует два блока: интро (заголовок + действия)
// и мета-карточку (PDF-иконка, инфо, статус, сетка метаданных).
//
// Не вынесен в widgets/ — специфичен только для ContractDetailPage.
//
// Data-honesty: тип договора, политика и уровень риска НЕ доступны в
// ContractDetails (только в пер-версионных AnalysisResults, FE-TASK-046/048) —
// показываем «—». Risk-бейдж (в Figma «Средний риск») опущен: данных нет,
// нейтральный фейк-бейдж добавил бы шум. Статус-бейдж — реальный
// (processing_status текущей версии). Стороны/тип в подзаголовке отсутствуют в
// API → подзаголовок не рендерим.
import { Link } from 'react-router-dom';

import type { ContractDetails } from '@/entities/contract';
import { StatusBadge } from '@/entities/version';
import { useCanExport } from '@/shared/auth';
import { Button, buttonVariants, Card } from '@/shared/ui';

export interface DocumentHeaderProps {
  contract: ContractDetails;
}

function formatDate(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' });
}

function formatDateTime(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleString('ru-RU', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function fileType(name?: string): string {
  if (!name) return '—';
  const ext = name.split('.').pop();
  return ext ? ext.toUpperCase() : '—';
}

export function DocumentHeader({ contract }: DocumentHeaderProps): JSX.Element {
  const canExport = useCanExport();
  const version = contract.current_version;
  const title = contract.title ?? 'Договор без названия';
  const contractId = contract.contract_id;
  const versionId = version?.version_id;
  const isReady = version?.processing_status === 'READY';
  const resultHref =
    contractId && versionId ? `/contracts/${contractId}/versions/${versionId}/result` : undefined;

  const docInfoSub =
    [
      formatDateTime(version?.created_at),
      version?.version_number ? `Версия ${version.version_number}` : null,
    ]
      .filter((v): v is string => Boolean(v) && v !== '—')
      .join(' · ') || '—';

  return (
    <div data-testid="document-header" className="flex flex-col gap-5">
      {/* Page Intro — заголовок + действия (Figma 309:8) */}
      <div className="flex flex-wrap items-start justify-between gap-4">
        <h1 className="text-24 font-bold leading-9 text-fg">{title}</h1>
        <div className="flex flex-wrap items-center gap-2.5 pt-1">
          {/* Экспорт/шаринг — только для ролей с правом экспорта (как
              ExportShareButton на ResultPage); иначе CTA не показываем. */}
          {canExport &&
            (isReady && resultHref ? (
              <>
                <Link
                  to={resultHref}
                  className={buttonVariants({ variant: 'secondary', size: 'sm' })}
                >
                  Скачать отчёт
                </Link>
                <Link
                  to={resultHref}
                  className={buttonVariants({ variant: 'secondary', size: 'sm' })}
                >
                  Поделиться
                </Link>
              </>
            ) : (
              <>
                <Button type="button" variant="secondary" size="sm" disabled>
                  Скачать отчёт
                </Button>
                <Button type="button" variant="secondary" size="sm" disabled>
                  Поделиться
                </Button>
              </>
            ))}
          {contractId ? (
            <Link
              to={`/contracts/new?contractId=${contractId}`}
              className={buttonVariants({ variant: 'primary', size: 'sm' })}
            >
              Повторная проверка
            </Link>
          ) : null}
        </div>
      </div>

      {/* Document Meta Card (Figma 310:2) */}
      <Card
        as="section"
        aria-label="Метаданные документа"
        radius="xl"
        className="flex flex-col gap-5 border border-border-subtle px-7 py-6 shadow-none"
      >
        <div className="flex flex-wrap items-center gap-4">
          <span
            aria-hidden
            className="flex h-12 w-12 shrink-0 items-center justify-center rounded-lg bg-brand-500/10 text-12 font-bold text-brand-600"
          >
            PDF
          </span>
          <div className="flex min-w-0 flex-col gap-1">
            <p className="text-17 font-semibold text-fg">{title}</p>
            <p className="text-13 text-fg-subtle">{docInfoSub}</p>
          </div>
          {version?.processing_status ? (
            <div className="ml-auto flex items-center gap-2">
              <StatusBadge status={version.processing_status} />
            </div>
          ) : null}
        </div>

        <div className="h-px w-full bg-border-subtle" />

        <dl className="flex flex-wrap gap-x-10 gap-y-4">
          <MetaItem label="Тип договора" value="—" />
          <MetaItem
            label="Текущая версия"
            value={version?.version_number ? `Версия ${version.version_number}` : '—'}
          />
          <MetaItem label="Дата загрузки" value={formatDate(version?.created_at)} />
          <MetaItem label="Последнее обновление" value={formatDateTime(contract.updated_at)} />
          <MetaItem label="Тип файла" value={fileType(version?.source_file_name)} />
          <MetaItem label="Политика" value="—" />
        </dl>
      </Card>
    </div>
  );
}

function MetaItem({ label, value }: { label: string; value: string }): JSX.Element {
  return (
    <div className="flex flex-col gap-1">
      <dt className="text-12 text-fg-subtle">{label}</dt>
      <dd className="text-14 font-medium text-fg">{value}</dd>
    </div>
  );
}
