// LastCheck — карточка «Последняя проверка» на ContractDetailPage.
// Отличается от widgets/dashboard-last-check — та собирает данные по списку
// /contracts, а эта — метаданные текущей версии договора.
import { Link } from 'react-router-dom';

import type { ContractDetails } from '@/entities/contract';
import { StatusBadge } from '@/entities/version';
import { buttonVariants } from '@/shared/ui';

export interface LastCheckProps {
  contract: ContractDetails;
}

function formatDate(iso?: string): string {
  if (!iso) return '—';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleDateString('ru-RU', { day: '2-digit', month: 'long', year: 'numeric' });
}

export function LastCheck({ contract }: LastCheckProps): JSX.Element {
  const version = contract.current_version;
  const contractId = contract.contract_id;
  const versionId = version?.version_id;
  const isReady = version?.processing_status === 'READY';

  return (
    <section
      aria-label="Последняя проверка"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header className="flex items-center justify-between gap-2">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Последняя проверка
        </h2>
        {version?.processing_status ? <StatusBadge status={version.processing_status} /> : null}
      </header>

      {!version ? (
        <p className="text-sm text-fg-muted">Версий ещё нет.</p>
      ) : (
        <div className="flex flex-col gap-2">
          <p className="text-base font-semibold text-fg">
            v{version.version_number ?? '—'}
            {version.source_file_name ? (
              <span className="ml-2 text-sm font-normal text-fg-muted">
                {version.source_file_name}
              </span>
            ) : null}
          </p>
          <p className="text-xs text-fg-muted">Создана: {formatDate(version.created_at)}</p>
          {version.processing_status_message ? (
            <p className="text-sm text-fg-muted">{version.processing_status_message}</p>
          ) : null}
          {isReady && contractId && versionId ? (
            <Link
              to={`/contracts/${contractId}/versions/${versionId}/result`}
              className={`${buttonVariants({ variant: 'primary', size: 'md' })} self-start`}
            >
              Открыть результат
            </Link>
          ) : null}
        </div>
      )}
    </section>
  );
}
