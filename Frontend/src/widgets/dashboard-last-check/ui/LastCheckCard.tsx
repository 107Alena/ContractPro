// LastCheckCard — «Последняя проверка» на главном экране (Figma 84:2 → 89:4).
//
// Показывает самый свежий договор из /contracts?size=5. Уровень риска и счётчики
// (высокий/средний/низкий) приходят из /risks — отложено (FE-TASK-046). Поэтому:
//   • бейдж в шапке — это СТАТУС обработки (реальные данные), не риск-уровень;
//   • счётчики рисков рендерятся структурно с «—» (данные появятся после анализа);
//   • описание — статусная подсказка, без выдуманного summary рисков.
//
// CTA реализованы как <Link className={buttonVariants(...)}> (не <Button asChild>):
// Button оборачивает children тремя слотами (iconLeft/children/iconRight), из-за
// чего Radix Slot падает на React.Children.only в jsdom. Прямая стилизация Link
// через buttonVariants — локальный обход.
import { Link } from 'react-router-dom';

import { type ContractSummary, viewStatus } from '@/entities/contract';
import { Badge, buttonVariants, Card, Spinner } from '@/shared/ui';

export interface LastCheckCardProps {
  contract?: ContractSummary | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

const RISK_LEVELS = [
  { key: 'high', label: 'высокий', dot: 'bg-risk-high' },
  { key: 'medium', label: 'средний', dot: 'bg-risk-medium' },
  { key: 'low', label: 'низкий', dot: 'bg-risk-low' },
] as const;

function formatDateTime(iso?: string): string | null {
  if (!iso) return null;
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return null;
  return date.toLocaleString('ru-RU', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function LastCheckCard({ contract, isLoading, error }: LastCheckCardProps): JSX.Element {
  const view = contract ? viewStatus(contract.processing_status) : null;

  return (
    <Card aria-label="Последняя проверка" className="flex flex-col gap-4 px-6 py-[22px]">
      <header className="flex items-center justify-between gap-2">
        <h2 className="text-17 font-semibold text-fg">Последняя проверка</h2>
        {view ? <Badge variant={view.tone}>{view.label}</Badge> : null}
      </header>

      {isLoading && !contract ? (
        <div className="flex min-h-[120px] items-center justify-center" aria-busy="true">
          <Spinner size="md" aria-hidden="true" />
        </div>
      ) : error ? (
        <p role="alert" className="text-14 text-danger">
          Не удалось загрузить последнюю проверку.
        </p>
      ) : !contract ? (
        <EmptyState />
      ) : (
        <Content contract={contract} />
      )}
    </Card>
  );
}

function EmptyState(): JSX.Element {
  return (
    <div className="flex flex-col items-start gap-3 py-4">
      <p className="text-14 text-fg-muted">
        Пока нет проверок. Загрузите договор — и через минуту увидите результат.
      </p>
      <Link to="/contracts/new" className={buttonVariants({ variant: 'primary', size: 'md' })}>
        Загрузить договор
      </Link>
    </div>
  );
}

function Content({ contract }: { contract: ContractSummary }): JSX.Element {
  const view = viewStatus(contract.processing_status);
  const isReady = view.bucket === 'ready';
  const isFailed = view.bucket === 'failed';
  const id = contract.contract_id;
  const version = contract.current_version_number;

  const meta = [
    formatDateTime(contract.updated_at ?? contract.created_at),
    version != null ? `Версия ${version}` : null,
  ]
    .filter(Boolean)
    .join('  ·  ');

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <p className="text-15 font-semibold text-fg">{contract.title ?? 'Договор без названия'}</p>
        {meta ? <p className="text-13 text-fg-subtle">{meta}</p> : null}
      </div>

      <p className="text-14 leading-5 text-fg-muted">
        {isFailed
          ? 'Обработка завершилась с ошибкой. Откройте карточку договора для подробностей.'
          : !isReady
            ? 'Идёт обработка — обновим результат автоматически.'
            : 'Откройте результат, чтобы увидеть риски и рекомендации.'}
      </p>

      {isReady ? (
        // Счётчики рисков — структура с «—»: high/medium/low появятся после /risks.
        <div className="flex flex-wrap gap-3" aria-label="Счётчики рисков (данные готовятся)">
          {RISK_LEVELS.map((level) => (
            <span
              key={level.key}
              className="inline-flex items-center gap-1.5 rounded-sm bg-bg-muted px-3 py-1.5"
            >
              <span className={`size-2 rounded-full ${level.dot}`} aria-hidden="true" />
              <span className="text-14 font-bold text-fg">—</span>
              <span className="text-13 text-fg-subtle">{level.label}</span>
            </span>
          ))}
        </div>
      ) : null}

      <div className="flex flex-wrap gap-2.5">
        {isReady && id ? (
          <>
            <Link
              to={`/contracts/${id}`}
              className={buttonVariants({ variant: 'primary', size: 'sm' })}
            >
              Открыть результат
            </Link>
            <Link
              to={`/contracts/${id}`}
              className={buttonVariants({ variant: 'secondary', size: 'sm' })}
            >
              Повторная проверка
            </Link>
            <Link
              to={`/contracts/${id}/compare`}
              className={buttonVariants({ variant: 'secondary', size: 'sm' })}
            >
              Сравнить версии
            </Link>
          </>
        ) : id ? (
          <Link
            to={`/contracts/${id}`}
            className={buttonVariants({ variant: 'secondary', size: 'sm' })}
          >
            Перейти к договору
          </Link>
        ) : null}
      </div>
    </div>
  );
}
