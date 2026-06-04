// SummaryCard — «Сводка по документу» (Figma 306:2 → Summary Panel 311:4).
// Полная структура Figma с честными empty-states: текст резюме, risk-счётчики
// и «обязательные условия / отклонения» НЕ доступны в ContractDetails (только
// в пер-версионных AnalysisResults — FE-TASK-046/048) → «—»/placeholder.
// Реальные данные: статус проверки (processing_status текущей версии) и CTA.
// Risk-чип в заголовке (Figma «Средний риск») опущен — уровень риска недоступен.
import { Link } from 'react-router-dom';

import type { ContractDetails } from '@/entities/contract';
import { type VersionStatus } from '@/entities/version';
import { statusMeta } from '@/shared/lib/status-view';
import { Button, buttonVariants, Card } from '@/shared/ui';

export interface SummaryCardProps {
  contract: ContractDetails;
  /** Query-suffix `?base=&target=` для пресета пары prev→current (Stage 5). */
  compareSearch?: string;
}

const RISK_LEVELS: ReadonlyArray<{ label: string }> = [
  { label: 'высокий' },
  { label: 'средний' },
  { label: 'низкий' },
];

function statusDotClass(status: VersionStatus | undefined | null): string {
  const { tone } = statusMeta(status);
  switch (tone) {
    case 'success':
      return 'bg-success';
    case 'warning':
      return 'bg-warning';
    case 'danger':
      return 'bg-danger';
    case 'brand':
      return 'bg-brand-500';
    default:
      return 'bg-fg-disabled';
  }
}

export function SummaryCard({ contract, compareSearch = '' }: SummaryCardProps): JSX.Element {
  const version = contract.current_version;
  const contractId = contract.contract_id;
  const versionId = version?.version_id;
  const status = version?.processing_status;
  const isReady = status === 'READY';
  const resultHref =
    contractId && versionId ? `/contracts/${contractId}/versions/${versionId}/result` : undefined;

  return (
    <Card
      as="section"
      aria-label="Сводка по документу"
      radius="xl"
      className="flex flex-col gap-5 border border-border-subtle px-7 py-6 shadow-none"
    >
      <h2 className="text-18 font-semibold text-fg">Сводка по документу</h2>

      <p className="text-15 leading-6 text-fg-strong">
        {isReady
          ? 'Развёрнутое резюме доступно в «Результате проверки» — откройте текущую версию для подробного разбора.'
          : 'Резюме появится после завершения анализа текущей версии.'}
      </p>

      {/* Risk-счётчики — структура Figma, значения «—» (данные после анализа). */}
      <ul className="flex flex-wrap gap-4" aria-label="Счётчики рисков (появятся после анализа)">
        {RISK_LEVELS.map((lvl) => (
          <li
            key={lvl.label}
            className="inline-flex items-center gap-1.5 rounded-md bg-bg-muted py-2 pl-3 pr-3.5"
          >
            <span aria-hidden className="h-2 w-2 rounded-full bg-fg-disabled" />
            <span className="text-16 font-bold text-fg">—</span>
            <span className="text-13 text-fg-muted">{lvl.label}</span>
          </li>
        ))}
      </ul>

      <dl className="flex flex-col gap-2.5">
        <StatusLine label="Обязательные условия:" value="—" dotClass="bg-fg-disabled" />
        <StatusLine label="Отклонения от политики:" value="—" dotClass="bg-fg-disabled" />
        <StatusLine
          label="Статус проверки:"
          value={statusMeta(status).label}
          dotClass={statusDotClass(status)}
        />
      </dl>

      <div className="flex flex-wrap items-center gap-3">
        {isReady && resultHref ? (
          <Link to={resultHref} className={buttonVariants({ variant: 'primary', size: 'md' })}>
            Открыть результат
          </Link>
        ) : (
          <Button type="button" variant="primary" size="md" disabled>
            Открыть результат
          </Button>
        )}
        {contractId ? (
          <Link
            to={`/contracts/new?contractId=${contractId}`}
            className={buttonVariants({ variant: 'secondary', size: 'md' })}
          >
            Повторная проверка
          </Link>
        ) : null}
        {contractId ? (
          <Link
            to={`/contracts/${contractId}/compare${compareSearch}`}
            className={buttonVariants({ variant: 'secondary', size: 'md' })}
          >
            Сравнить версии
          </Link>
        ) : null}
      </div>
    </Card>
  );
}

function StatusLine({
  label,
  value,
  dotClass,
}: {
  label: string;
  value: string;
  dotClass: string;
}): JSX.Element {
  return (
    <div className="flex items-center gap-2">
      <dt className="text-13 text-fg-subtle">{label}</dt>
      <dd className="flex items-center gap-2 text-13 font-medium text-fg">
        <span aria-hidden className={`h-1.5 w-1.5 rounded-full ${dotClass}`} />
        {value}
      </dd>
    </div>
  );
}
