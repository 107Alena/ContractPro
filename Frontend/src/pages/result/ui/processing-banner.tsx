// ProcessingBanner — обёртка над ProcessingProgress для экрана «Результат»
// в состоянии PROCESSING/QUEUED/ANALYZING/GENERATING_REPORTS. Добавляет
// заголовок и короткий объясняющий текст. SSE в parent-page уже обновляет
// processing_status через qk.contracts.status (§7.7).
import { Link } from 'react-router-dom';

import type { components } from '@/shared/api/openapi';
import { buttonVariants } from '@/shared/ui/button';
import { ProcessingProgress } from '@/widgets/processing-progress';

type Status = components['schemas']['UserProcessingStatus'];

export interface ProcessingBannerProps {
  status: Status;
  contractId: string;
  /** Текстовое сообщение на русском (из processing_status_message бэкенда). */
  message?: string | undefined;
}

export function ProcessingBanner({
  status,
  contractId,
  message,
}: ProcessingBannerProps): JSX.Element {
  return (
    <section
      aria-label="Обработка договора"
      data-testid="state-processing"
      className="flex flex-col gap-3"
    >
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-semibold text-fg">Обрабатываем договор</h2>
        <p className="text-sm text-fg-muted">
          Результаты появятся автоматически. Обновление идёт в реальном времени.
        </p>
      </div>
      <ProcessingProgress
        status={status}
        awaitingAction={
          <Link
            to={`/contracts/${contractId}`}
            className={buttonVariants({ variant: 'secondary', size: 'md' })}
            data-testid="processing-banner-awaiting-link"
          >
            Перейти к подтверждению типа
          </Link>
        }
      />
      {message ? <p className="text-xs text-fg-muted">{message}</p> : null}
    </section>
  );
}
