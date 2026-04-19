// RecommendationsList — список рекомендаций по правке формулировок (экран 8
// Figma; §17.4, §17.5 artifact RECOMMENDATIONS). В v1 FE-TASK-045 без
// собственного useQuery: ожидает `items` пропом. Реальное подключение
// /recommendations — FE-TASK-046.
//
// RBAC: виджет скрывается на уровне page через <Can I="recommendations.view">
// (Pattern B из §5.6.1).
//
// TODO(FE-TASK-046/048): useRecommendations(...) в parent-page должен быть
// защищён `enabled: useCan('recommendations.view')` — §5.6.1 требует, чтобы
// скрытые для роли данные не загружались. Не переносить useQuery внутрь
// этого виджета без такого guard'а.
import type { components } from '@/shared/api/openapi';
import { Spinner } from '@/shared/ui';

type Recommendation = components['schemas']['Recommendation'];

export interface RecommendationsListProps {
  items?: readonly Recommendation[] | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

export function RecommendationsList({
  items,
  isLoading,
  error,
}: RecommendationsListProps): JSX.Element {
  return (
    <section
      aria-label="Рекомендации"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header>
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Рекомендации
        </h2>
        <p className="mt-1 text-xs text-fg-muted">
          Предлагаемые правки формулировок для снижения рисков
        </p>
      </header>

      {isLoading && !items ? (
        <div
          data-testid="recommendations-list-loading"
          className="flex min-h-[120px] items-center justify-center"
          aria-busy="true"
        >
          <Spinner size="md" aria-hidden="true" />
        </div>
      ) : error ? (
        <p role="alert" className="text-sm text-danger">
          Не удалось загрузить рекомендации.
        </p>
      ) : !items || items.length === 0 ? (
        <p className="text-sm text-fg-muted" data-testid="recommendations-list-empty">
          Рекомендации появятся после завершения анализа текущей версии.
        </p>
      ) : (
        <ul className="flex flex-col gap-3" data-testid="recommendations-list">
          {items.map((rec, idx) => (
            <RecommendationItem key={rec.risk_id ?? `rec-${idx}`} rec={rec} />
          ))}
        </ul>
      )}
    </section>
  );
}

function RecommendationItem({ rec }: { rec: Recommendation }): JSX.Element {
  return (
    <li
      data-testid="recommendations-list-item"
      className="flex flex-col gap-2 rounded-md border border-border bg-bg-muted p-3"
    >
      {rec.original_text ? (
        <div className="flex flex-col gap-1">
          <p className="text-xs font-medium uppercase tracking-wide text-fg-muted">Было</p>
          <p className="text-sm text-fg">{rec.original_text}</p>
        </div>
      ) : null}
      {rec.recommended_text ? (
        <div className="flex flex-col gap-1">
          <p className="text-xs font-medium uppercase tracking-wide text-success">Рекомендуем</p>
          <p className="text-sm text-fg">{rec.recommended_text}</p>
        </div>
      ) : null}
      {rec.explanation ? <p className="text-xs text-fg-muted">{rec.explanation}</p> : null}
    </li>
  );
}
