// DocumentCard — шапка экрана результатов (§16.5 дерево ResultPage, §17.5
// artifact CLASSIFICATION_RESULT, row 6). Показывает название договора,
// определённый тип + confidence, статус обработки версии и метаинформацию.
//
// Page-local компонент: компоновка специфична именно для экрана «Результат»
// и отличается от DocumentHeader на карточке договора (там другой набор
// полей + DocumentStatus-badge вместо confidence).
import type { ContractDetails } from '@/entities/contract';
import type { AnalysisResults } from '@/entities/result';
import { StatusBadge } from '@/entities/version';
import { Badge } from '@/shared/ui/badge';

export interface DocumentCardProps {
  contract: ContractDetails;
  results?: AnalysisResults | undefined;
}

function formatDate(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleDateString('ru-RU', { day: '2-digit', month: 'long', year: 'numeric' });
}

function classificationConfidenceTone(conf: number): 'success' | 'warning' | 'danger' {
  if (conf >= 0.85) return 'success';
  if (conf >= 0.6) return 'warning';
  return 'danger';
}

export function DocumentCard({ contract, results }: DocumentCardProps): JSX.Element {
  const version = contract.current_version;
  const title = contract.title ?? 'Договор без названия';
  const classification = results?.contract_type;
  const confidence =
    typeof classification?.confidence === 'number' ? classification.confidence : undefined;

  return (
    <header
      data-testid="document-card"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <div className="flex flex-wrap items-center gap-2">
        <h1 className="text-2xl font-semibold text-fg">{title}</h1>
        {version?.processing_status ? <StatusBadge status={version.processing_status} /> : null}
        {classification?.contract_type ? (
          <Badge variant="brand" data-testid="document-card-type">
            {classification.contract_type}
          </Badge>
        ) : null}
        {confidence !== undefined ? (
          <Badge
            variant={classificationConfidenceTone(confidence)}
            data-testid="document-card-confidence"
          >
            {Math.round(confidence * 100)}%
          </Badge>
        ) : null}
      </div>
      <dl className="grid grid-cols-[max-content,1fr] gap-x-4 gap-y-1 text-sm text-fg-muted md:grid-cols-[max-content,1fr,max-content,1fr]">
        <dt>Версия:</dt>
        <dd className="text-fg">{version?.version_number ? `v${version.version_number}` : '—'}</dd>
        <dt>Обновлён:</dt>
        <dd className="text-fg">{formatDate(contract.updated_at)}</dd>
        {version?.source_file_name ? (
          <>
            <dt>Исходный файл:</dt>
            <dd className="text-fg">{version.source_file_name}</dd>
          </>
        ) : null}
        {version?.created_at ? (
          <>
            <dt>Загружено:</dt>
            <dd className="text-fg">{formatDate(version.created_at)}</dd>
          </>
        ) : null}
      </dl>
    </header>
  );
}
