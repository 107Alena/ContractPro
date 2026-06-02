// DocumentCard — тонкая мета-карточка документа на экране результатов
// (Figma 150:2 → DocumentMetaCard 153:20). Одна строка: PDF-иконка + название
// договора (тип · дата · версия) + статус-бейдж. Заголовок страницы (h1) живёт
// в Page Intro (ResultPage PageHeader) — здесь название рендерится как <p>,
// чтобы не плодить второй h1.
//
// Page-local: компоновка специфична экрану «Результат». Тип договора
// (results.contract_type) встраивается в подпись; confidence в Figma-мете не
// показывается (деталь классификации) — опущена.
import type { ContractDetails } from '@/entities/contract';
import type { AnalysisResults } from '@/entities/result';
import { StatusBadge } from '@/entities/version';
import { Card } from '@/shared/ui';

export interface DocumentCardProps {
  contract: ContractDetails;
  results?: AnalysisResults | undefined;
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

export function DocumentCard({ contract, results }: DocumentCardProps): JSX.Element {
  const version = contract.current_version;
  const title = contract.title ?? 'Договор без названия';
  const contractType = results?.contract_type?.contract_type;

  const sub = [
    contractType,
    version?.created_at ? formatDateTime(version.created_at) : null,
    version?.version_number ? `Версия ${version.version_number}` : null,
  ]
    .filter((v): v is string => Boolean(v))
    .join(' · ');

  return (
    <Card
      as="header"
      data-testid="document-card"
      radius="card"
      className="flex flex-wrap items-center gap-5 border border-border-subtle px-5 py-4 shadow-none"
    >
      <span
        aria-hidden
        className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg bg-risk-high-bg text-11 font-bold text-risk-high"
      >
        PDF
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <p className="truncate text-14 font-medium text-fg">{title}</p>
        <p className="truncate text-12 text-fg-muted">{sub || '—'}</p>
      </div>
      {version?.processing_status ? <StatusBadge status={version.processing_status} /> : null}
    </Card>
  );
}
