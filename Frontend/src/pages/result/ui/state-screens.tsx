// State-screens для ResultPage: Loading / NotFound / Error / Rejected /
// AwaitingInput / Failed. Собраны в одном файле для удобства импорта в
// ResultPage.tsx (паттерн ComparisonPage/ContractDetailPage).
import type { ReactNode } from 'react';
import { Link } from 'react-router-dom';

import { buttonVariants } from '@/shared/ui/button';
import { Spinner } from '@/shared/ui/spinner';

export function LoadingState(): JSX.Element {
  return (
    <section
      data-testid="state-loading"
      aria-busy="true"
      aria-label="Загрузка результатов"
      className="flex min-h-[240px] flex-col items-center justify-center gap-3 rounded-md border border-border bg-bg p-8"
    >
      <Spinner size="lg" aria-hidden="true" />
      <p className="text-sm text-fg-muted">Загружаем результаты анализа…</p>
    </section>
  );
}

export function NotFoundState({ contractId }: { contractId: string | undefined }): JSX.Element {
  return (
    <section
      data-testid="state-not-found"
      className="flex flex-col items-center justify-center gap-3 rounded-md border border-dashed border-border bg-bg-muted p-12 text-center"
    >
      <h2 className="text-lg font-semibold text-fg">Результаты не найдены</h2>
      <p className="max-w-md text-sm text-fg-muted">
        Возможно, версия удалена или ссылка устарела. Вернитесь на карточку договора и выберите
        актуальную версию.
      </p>
      <Link
        to={contractId ? `/contracts/${contractId}` : '/contracts'}
        className={buttonVariants({ variant: 'primary', size: 'md' })}
      >
        К карточке договора
      </Link>
    </section>
  );
}

export function ErrorState({
  message,
  actions,
}: {
  message: string;
  actions?: ReactNode;
}): JSX.Element {
  return (
    <section
      data-testid="state-error"
      role="alert"
      className="flex flex-col items-center justify-center gap-3 rounded-md border border-danger/30 bg-bg p-12 text-center"
    >
      <h2 className="text-lg font-semibold text-danger">Не удалось получить результаты</h2>
      <p className="max-w-md text-sm text-fg-muted">{message}</p>
      {actions ? <div className="flex flex-wrap justify-center gap-2">{actions}</div> : null}
    </section>
  );
}

export function FailedState({
  message,
  recheckButton,
}: {
  message?: string | undefined;
  recheckButton: ReactNode;
}): JSX.Element {
  return (
    <section
      data-testid="state-failed"
      className="flex flex-col items-center justify-center gap-3 rounded-md border border-danger/30 bg-bg p-12 text-center"
    >
      <h2 className="text-lg font-semibold text-danger">Обработка завершилась с ошибкой</h2>
      <p className="max-w-md text-sm text-fg-muted">
        {message ??
          'Мы не смогли проанализировать договор. Проверьте корректность файла и запустите проверку заново.'}
      </p>
      <div className="flex flex-wrap justify-center gap-2">{recheckButton}</div>
    </section>
  );
}

export function RejectedState({
  message,
  contractId,
}: {
  message?: string | undefined;
  contractId: string | undefined;
}): JSX.Element {
  return (
    <section
      data-testid="state-rejected"
      className="flex flex-col items-center justify-center gap-3 rounded-md border border-danger/30 bg-bg p-12 text-center"
    >
      <h2 className="text-lg font-semibold text-danger">Файл отклонён</h2>
      <p className="max-w-md text-sm text-fg-muted">
        {message ??
          'Не удалось загрузить файл: некорректный формат или повреждённый PDF. Загрузите новую версию договора.'}
      </p>
      <Link
        to={contractId ? `/contracts/${contractId}` : '/contracts/new'}
        className={buttonVariants({ variant: 'primary', size: 'md' })}
        data-testid="rejected-replace-link"
      >
        Заменить файл
      </Link>
    </section>
  );
}

export function AwaitingInputState({
  contractId,
  message,
}: {
  contractId: string;
  message?: string | undefined;
}): JSX.Element {
  return (
    <section
      data-testid="state-awaiting"
      className="flex flex-col items-center justify-center gap-3 rounded-md border border-warning/50 bg-bg p-12 text-center"
    >
      <h2 className="text-lg font-semibold text-fg">Требуется подтверждение</h2>
      <p className="max-w-md text-sm text-fg-muted">
        {message ??
          'Модель классификации не уверена в типе договора. Подтвердите тип, чтобы продолжить анализ.'}
      </p>
      <Link
        to={`/contracts/${contractId}`}
        className={buttonVariants({ variant: 'primary', size: 'md' })}
        data-testid="awaiting-confirm-link"
      >
        Подтвердить тип договора
      </Link>
    </section>
  );
}
