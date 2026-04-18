// LastCheckCard — «последняя проверка» на главном экране (§17.4).
//
// Показывает самый свежий договор из списка /contracts?size=5: название, статус
// через Badge, CTA «Открыть результат». Rich-данные AggregateScore/RiskProfile
// требуют /results endpoint — отложено в FE-TASK-046 (ResultPage).
//
// CTA-кнопки реализованы как <Link className={buttonVariants(...)}>, а не через
// <Button asChild>. Причина: Button.tsx оборачивает children тремя JSX-слотами
// (iconLeft / children / iconRight) — при asChild Slot в jsdom (Radix 1.2+)
// падает на `React.Children.only` из-за множественных children даже если лишние
// — undefined. Пограничный случай, не блокирующий asChild в целом; локальный
// workaround — прямая стилизация Link через `buttonVariants`.
import { Link } from 'react-router-dom';

import { type ContractSummary, viewStatus } from '@/entities/contract';
import { Badge, buttonVariants, Spinner } from '@/shared/ui';

export interface LastCheckCardProps {
  contract?: ContractSummary | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

export function LastCheckCard({ contract, isLoading, error }: LastCheckCardProps): JSX.Element {
  return (
    <section
      aria-label="Последняя проверка"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header className="flex items-center justify-between gap-2">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Последняя проверка
        </h2>
      </header>

      {isLoading && !contract ? (
        <div className="flex min-h-[120px] items-center justify-center" aria-busy="true">
          <Spinner size="md" aria-hidden="true" />
        </div>
      ) : error ? (
        <p role="alert" className="text-sm text-danger">
          Не удалось загрузить последнюю проверку.
        </p>
      ) : !contract ? (
        <EmptyState />
      ) : (
        <Content contract={contract} />
      )}
    </section>
  );
}

function EmptyState(): JSX.Element {
  return (
    <div className="flex flex-col items-start gap-3 py-4">
      <p className="text-base text-fg-muted">
        Пока нет проверок. Загрузите договор — и через минуту увидите результат.
      </p>
      <Link to="/contracts/new" className={buttonVariants({ variant: 'primary', size: 'md' })}>
        Загрузить договор
      </Link>
    </div>
  );
}

function Content({ contract }: { contract: ContractSummary }): JSX.Element {
  const status = contract.processing_status;
  const view = viewStatus(status);
  const isReady = view.bucket === 'ready';
  const isTerminalFailure = view.bucket === 'failed';

  const contractId = contract.contract_id;
  const versionNumber = contract.current_version_number ?? null;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 className="text-lg font-semibold text-fg">
          {contract.title ?? 'Договор без названия'}
        </h3>
        <Badge variant={view.tone}>{view.label}</Badge>
      </div>

      {status && !isReady && !isTerminalFailure ? (
        <p className="text-sm text-fg-muted">Идёт обработка — обновим результат автоматически.</p>
      ) : null}

      {isTerminalFailure ? (
        <p className="text-sm text-danger">
          Обработка завершилась с ошибкой. Откройте карточку договора для подробностей.
        </p>
      ) : null}

      <div className="flex gap-2">
        {isReady && contractId && versionNumber !== null ? (
          <Link
            to={`/contracts/${contractId}`}
            className={buttonVariants({ variant: 'primary', size: 'md' })}
          >
            Открыть результат
          </Link>
        ) : contractId ? (
          <Link
            to={`/contracts/${contractId}`}
            className={buttonVariants({ variant: 'secondary', size: 'md' })}
          >
            Перейти к договору
          </Link>
        ) : null}
      </div>
    </div>
  );
}
