// KeyRisksCards — секция «Ключевые риски за последнее время» (Figma 84:2 → 92:2).
//
// Детальные риски (уровень / заголовок / описание / договор) приходят из /risks —
// отложено (FE-TASK-046). Figma-структура из 4 risk-карточек рендерится
// skeleton-плейсхолдерами (данные готовятся), пустой список — текстовый
// empty-state. Никаких выдуманных рисков.
import { type ContractSummary } from '@/entities/contract';
import { Card, Spinner } from '@/shared/ui';

export interface KeyRisksCardsProps {
  items?: readonly ContractSummary[] | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

const SKELETON_KEYS = ['a', 'b', 'c', 'd'] as const;

export function KeyRisksCards({ items, isLoading, error }: KeyRisksCardsProps): JSX.Element {
  return (
    <section aria-label="Ключевые риски" className="flex flex-col gap-4">
      <h2 className="text-17 font-semibold text-fg">Ключевые риски за последнее время</h2>

      {isLoading && !items ? (
        <div className="flex min-h-[120px] items-center justify-center" aria-busy="true">
          <Spinner size="md" aria-hidden="true" />
        </div>
      ) : error ? (
        <p role="alert" className="text-14 text-danger">
          Не удалось загрузить данные о рисках.
        </p>
      ) : !items || items.length === 0 ? (
        <p className="text-13 text-fg-muted">Риски появятся после первой проверки.</p>
      ) : (
        <>
          <p className="text-13 text-fg-muted">
            Детальные риски доступны в результатах проверки — здесь они появятся после анализа.
          </p>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {SKELETON_KEYS.map((key) => (
              <RiskCardSkeleton key={key} />
            ))}
          </div>
        </>
      )}
    </section>
  );
}

function RiskCardSkeleton(): JSX.Element {
  return (
    <Card as="article" radius="md" aria-hidden="true" className="flex flex-col gap-2 p-4">
      <span className="h-5 w-16 rounded-sm bg-bg-muted" />
      <span className="mt-1 h-3 w-3/4 rounded bg-bg-muted" />
      <span className="h-3 w-1/2 rounded bg-bg-muted" />
      <span className="h-2.5 w-full rounded bg-bg-muted" />
    </Card>
  );
}
