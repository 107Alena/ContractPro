// RisksList — секция «Ключевые риски» (Figma 150:2 → 156:2/156:7). Каждый риск
// — карточка с левым акцент-бордером по уровню, бейджем, описанием, строкой
// «пункт · рекомендация» и действиями «Подробнее» (drawer) / «Показать
// формулировку» (раскрывает рекомендованную формулировку).
//
// Рекомендации интегрированы в карточки риска по `recommendation.risk_id ===
// risk.id` (Figma 150:2 не имеет отдельной секции «Рекомендации»). Несколько
// рекомендаций на один risk_id группируются и показываются все (не теряются).
// Передаётся опциональным `recommendations` — ContractDetail/Dashboard их не
// передают и получают карточки без рекомендательной части (обратная совм.).
//
// RBAC: виджет скрывается на уровне page через <Can I="risks.view"> (§5.6.1).
import { useId, useState } from 'react';

import { RiskBadge, type RiskLevel } from '@/entities/risk';
import type { components } from '@/shared/api/openapi';
import { cn } from '@/shared/lib/cn';
import { Spinner } from '@/shared/ui';

type Risk = components['schemas']['Risk'];
type Recommendation = components['schemas']['Recommendation'];

export interface RisksListProps {
  risks?: readonly Risk[] | undefined;
  /** Рекомендации версии — мапятся в карточки по `risk_id`. */
  recommendations?: readonly Recommendation[] | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
  /**
   * Открывает RiskDetailsDrawer. Когда передан И у риска есть `id` — у карточки
   * появляется кнопка «Подробнее» (data-testid=risks-list-item-button). Без id
   * кнопку не рендерим (клик был бы silent no-op: drawer открывается по id).
   */
  onRiskClick?: ((risk: Risk) => void) | undefined;
}

const RISK_ACCENT: Record<RiskLevel, string> = {
  high: 'border-l-risk-high',
  medium: 'border-l-risk-medium',
  low: 'border-l-risk-low',
};

export function RisksList({
  risks,
  recommendations,
  isLoading,
  error,
  onRiskClick,
}: RisksListProps): JSX.Element {
  // Группируем по risk_id (несколько рекомендаций на риск — допустимо схемой).
  const recsByRiskId = new Map<string, Recommendation[]>();
  for (const rec of recommendations ?? []) {
    if (!rec.risk_id) continue;
    const list = recsByRiskId.get(rec.risk_id);
    if (list) list.push(rec);
    else recsByRiskId.set(rec.risk_id, [rec]);
  }
  const count = risks?.length ?? 0;

  return (
    <section
      aria-label="Ключевые риски"
      className="flex flex-col gap-4 rounded-xl border border-border-subtle bg-bg p-6 shadow-none"
    >
      <header className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-18 font-semibold text-fg">Ключевые риски</h2>
        {count > 0 ? <span className="text-13 text-fg-muted">{count} найдено</span> : null}
      </header>

      {isLoading && !risks ? (
        <div
          data-testid="risks-list-loading"
          className="flex min-h-[120px] items-center justify-center"
          aria-busy="true"
        >
          <Spinner size="md" aria-hidden="true" />
        </div>
      ) : error ? (
        <p role="alert" className="text-13 text-danger">
          Не удалось загрузить список рисков.
        </p>
      ) : !risks || risks.length === 0 ? (
        <p className="text-14 text-fg-muted" data-testid="risks-list-empty">
          Риски появятся после завершения анализа текущей версии.
        </p>
      ) : (
        <ul className="flex flex-col gap-3" data-testid="risks-list">
          {risks.map((risk, idx) => (
            <RiskItem
              key={risk.id ?? `risk-${idx}`}
              risk={risk}
              recommendations={(risk.id ? recsByRiskId.get(risk.id) : undefined) ?? []}
              {...(onRiskClick && risk.id ? { onClick: () => onRiskClick(risk) } : {})}
            />
          ))}
        </ul>
      )}
    </section>
  );
}

interface RiskItemProps {
  risk: Risk;
  recommendations: readonly Recommendation[];
  onClick?: () => void;
}

function RiskItem({ risk, recommendations, onClick }: RiskItemProps): JSX.Element {
  const [showWording, setShowWording] = useState(false);
  const wordingId = useId();
  const level = risk.level;
  const explanation = recommendations.find((r) => r.explanation)?.explanation;
  const wordingRecs = recommendations.filter((r) => r.recommended_text || r.original_text);
  const hasWording = wordingRecs.length > 0;

  return (
    <li
      data-testid="risks-list-item"
      className={cn(
        'flex flex-col gap-3 rounded-xl border border-border-subtle border-l-[3px] bg-bg px-5 py-4',
        level ? RISK_ACCENT[level] : 'border-l-border',
      )}
    >
      <div className="flex flex-wrap items-center gap-2.5">
        {level ? <RiskBadge level={level} /> : null}
        {risk.description ? (
          <p className="text-15 font-semibold text-fg">{risk.description}</p>
        ) : null}
      </div>

      {risk.legal_basis ? (
        <p className="text-13 leading-5 text-fg-muted">{risk.legal_basis}</p>
      ) : null}

      {risk.clause_ref || explanation ? (
        <div className="flex flex-wrap items-center gap-2 text-12">
          {risk.clause_ref ? (
            <span className="font-medium text-fg-subtle">{risk.clause_ref}</span>
          ) : null}
          {risk.clause_ref && explanation ? (
            <span aria-hidden className="text-fg-disabled">
              ·
            </span>
          ) : null}
          {explanation ? <span className="text-fg-muted">{explanation}</span> : null}
        </div>
      ) : null}

      {onClick || hasWording ? (
        <div className="flex flex-wrap items-center gap-4 text-13 font-medium">
          {onClick ? (
            <button
              type="button"
              onClick={onClick}
              data-testid="risks-list-item-button"
              className="rounded-sm text-brand-600 underline underline-offset-2 focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1"
            >
              Подробнее
            </button>
          ) : null}
          {hasWording ? (
            <button
              type="button"
              onClick={() => setShowWording((v) => !v)}
              aria-expanded={showWording}
              aria-controls={wordingId}
              className="rounded-sm text-fg-muted hover:text-fg focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1"
            >
              {showWording ? 'Скрыть формулировку' : 'Показать формулировку'}
            </button>
          ) : null}
        </div>
      ) : null}

      {hasWording ? (
        <div id={wordingId} hidden={!showWording} className="flex flex-col gap-3">
          {wordingRecs.map((rec, i) => (
            <div key={i} className="flex flex-col gap-2 rounded-md bg-bg-muted p-3 text-13">
              {rec.original_text ? (
                <p className="text-fg-muted line-through">{rec.original_text}</p>
              ) : null}
              {rec.recommended_text ? <p className="text-fg">{rec.recommended_text}</p> : null}
            </div>
          ))}
        </div>
      ) : null}
    </li>
  );
}
