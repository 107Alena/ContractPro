// SummaryCard — «Резюме договора» на карточке (экран 8 Figma, §17.4).
// В v1 FE-TASK-045 настоящий summary (/summary endpoint) не подключён — это
// FE-TASK-046 (ResultPage). Здесь показываем краткое описание того, что
// будет известно после анализа, и фиксируем место под контент.
import type { ContractDetails } from '@/entities/contract';

export interface SummaryCardProps {
  contract: ContractDetails;
}

export function SummaryCard({ contract }: SummaryCardProps): JSX.Element {
  const version = contract.current_version;
  const isReady = version?.processing_status === 'READY';

  return (
    <section
      aria-label="Резюме договора"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header>
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Резюме договора
        </h2>
        <p className="mt-1 text-xs text-fg-muted">
          Краткое описание на простом языке: стороны, предмет, ключевые условия
        </p>
      </header>
      {isReady ? (
        <p className="text-sm text-fg-muted">
          Развёрнутое резюме доступно в «Результате проверки» — откройте текущую версию для
          подробного разбора.
        </p>
      ) : (
        <p className="text-sm text-fg-muted">
          Резюме появится после завершения анализа текущей версии.
        </p>
      )}
    </section>
  );
}
