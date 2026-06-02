// LastCheck — карточка «Последняя проверка» (Figma 306:2 → Latest Check 313:2).
// Реальные данные: номер/дата текущей версии, статус, статус-сообщение, CTA.
// Risk-meta строка Figma (2 высокий / 3 средний / 1 низкий / 4 рекомендации) —
// уровни риска недоступны в ContractDetails (FE-TASK-046/048) → опущена.
import { Link } from 'react-router-dom';

import type { ContractDetails } from '@/entities/contract';
import { StatusBadge } from '@/entities/version';
import { useCanExport } from '@/shared/auth';
import { buttonVariants, Card } from '@/shared/ui';

export interface LastCheckProps {
  contract: ContractDetails;
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

export function LastCheck({ contract }: LastCheckProps): JSX.Element {
  const canExport = useCanExport();
  const version = contract.current_version;
  const contractId = contract.contract_id;
  const versionId = version?.version_id;
  const isReady = version?.processing_status === 'READY';
  const resultHref =
    contractId && versionId ? `/contracts/${contractId}/versions/${versionId}/result` : undefined;

  return (
    <Card
      as="section"
      aria-label="Последняя проверка"
      radius="xl"
      className="flex flex-col gap-4 border border-border-subtle px-7 py-6 shadow-none"
    >
      <header className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-18 font-semibold text-fg">Последняя проверка</h2>
        <div className="flex items-center gap-3">
          {version?.created_at ? (
            <span className="text-13 text-fg-subtle">{formatDateTime(version.created_at)}</span>
          ) : null}
          {version?.processing_status ? <StatusBadge status={version.processing_status} /> : null}
        </div>
      </header>

      {!version ? (
        <p className="text-14 text-fg-muted">Версий ещё нет.</p>
      ) : (
        <>
          <p className="text-14 leading-5 text-fg-muted">
            {version.processing_status_message ??
              (isReady
                ? 'Анализ завершён. Откройте результат проверки для детального разбора рисков и рекомендаций.'
                : 'Дождитесь завершения анализа текущей версии.')}
          </p>

          <div className="flex flex-wrap items-center gap-3">
            {isReady && resultHref ? (
              <Link to={resultHref} className={buttonVariants({ variant: 'primary', size: 'sm' })}>
                Открыть результат
              </Link>
            ) : null}
            {isReady && resultHref && canExport ? (
              <Link
                to={resultHref}
                className={buttonVariants({ variant: 'secondary', size: 'sm' })}
              >
                Скачать отчёт
              </Link>
            ) : null}
            {contractId ? (
              <Link
                to={`/contracts/${contractId}/compare`}
                className={buttonVariants({ variant: 'secondary', size: 'sm' })}
              >
                Открыть сравнение
              </Link>
            ) : null}
          </div>
        </>
      )}
    </Card>
  );
}
