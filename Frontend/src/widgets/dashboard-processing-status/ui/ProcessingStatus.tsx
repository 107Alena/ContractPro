// ProcessingStatus — карточка «Статус обработки» на dashboard (Figma 84:2 → 91:16).
//
// Показывает пошаговый прогресс активной (in_progress/awaiting) проверки из
// /contracts?size=5. Шаги выводятся из РЕАЛЬНОГО processing_status, монотонно и
// БЕЗ завышения прогресса. Шаги переименованы под реальные фазы пайплайна
// (status-view §5.2): Загружен → Извлечение текста → Юр. анализ → Отчёт, чтобы
// статус не «перескакивал» дальше реального этапа. Нет активных — empty-state.
import { type ContractSummary, viewStatus } from '@/entities/contract';
import { Card, Spinner } from '@/shared/ui';

export interface ProcessingStatusProps {
  items?: readonly ContractSummary[] | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

const STEPS = ['Загружен', 'Извлечение текста', 'Юр. анализ', 'Отчёт'] as const;

// Индекс активного шага из статуса (шаги < active → done, == active → active).
// Канон фаз (openapi UserProcessingStatus): UPLOADED→QUEUED→PROCESSING(извлечение)
// →ANALYZING(юр-анализ)→AWAITING_USER_INPUT(подтверждение типа)→GENERATING_REPORTS.
// Вызывается только для активных проверок (in_progress/awaiting).
function activeStepIndex(status: ContractSummary['processing_status']): number {
  switch (status) {
    case 'GENERATING_REPORTS':
      return 3;
    case 'ANALYZING':
    case 'AWAITING_USER_INPUT':
      return 2;
    case 'UPLOADED':
    case 'QUEUED':
    case 'PROCESSING':
    default:
      return 1;
  }
}

function findActive(items: readonly ContractSummary[]): ContractSummary | undefined {
  return items.find((item) => {
    const { bucket } = viewStatus(item.processing_status);
    return bucket === 'in_progress' || bucket === 'awaiting';
  });
}

export function ProcessingStatus({ items, isLoading, error }: ProcessingStatusProps): JSX.Element {
  const list = items ?? [];
  const active = findActive(list);

  return (
    <Card as="article" aria-label="Статус обработки" className="flex flex-col gap-3 p-5">
      <h2 className="text-15 font-semibold text-fg">Статус обработки</h2>

      {isLoading && list.length === 0 ? (
        <div className="flex min-h-[60px] items-center justify-center" aria-busy="true">
          <Spinner size="sm" aria-hidden="true" />
          <span className="sr-only">Загрузка статуса обработки…</span>
        </div>
      ) : error ? (
        <p role="alert" className="text-14 text-danger">
          Не удалось загрузить статус обработки.
        </p>
      ) : !active ? (
        <p className="text-13 text-fg-muted">Сейчас нет активных проверок.</p>
      ) : (
        <Steps contract={active} />
      )}
    </Card>
  );
}

function Steps({ contract }: { contract: ContractSummary }): JSX.Element {
  const current = activeStepIndex(contract.processing_status);

  return (
    <div className="flex flex-col gap-3">
      <ol className="flex flex-col gap-2.5">
        {STEPS.map((label, index) => {
          const done = index < current;
          const isActive = index === current;
          return (
            <li key={label} className="flex items-center gap-2.5">
              <span
                className={`grid size-5 shrink-0 place-items-center rounded-full text-11 font-semibold ${
                  done
                    ? 'bg-success text-white'
                    : isActive
                      ? 'bg-brand-500 text-white'
                      : 'bg-bg-muted text-fg-subtle'
                }`}
                aria-hidden="true"
              >
                {done ? '✓' : index + 1}
              </span>
              <span className={`text-14 ${done || isActive ? 'text-fg' : 'text-fg-subtle'}`}>
                {label}
              </span>
            </li>
          );
        })}
      </ol>
      <p className="text-13 text-fg-subtle">{contract.title ?? 'Договор без названия'}</p>
    </div>
  );
}
