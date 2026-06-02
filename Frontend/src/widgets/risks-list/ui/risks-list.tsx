// RisksList — секция «Ключевые риски» (Figma 150:2 → 156:2/156:7). Каждый риск
// — карточка с левым акцент-бордером по уровню, бейджем, описанием, строкой
// «пункт · рекомендация» и действиями «Подробнее» (drawer) / «Показать
// формулировку» (раскрывает рекомендованную формулировку).
//
// Рекомендации интегрированы в карточки риска по `recommendation.risk_id ===
// risk.id` (Figma 150:2 не имеет отдельной секции «Рекомендации»). Передаётся
// опциональным `recommendations` — ContractDetail/Dashboard их не передают и
// получают карточки без рекомендательной части (обратная совместимость).
//
// RBAC: виджет скрывается на уровне page через <Can I="risks.view"> (§5.6.1).
import { useState } from 'react';

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
   * Открывает RiskDetailsDrawer. Когда передан — у карточки появляется кнопка
   * «Подробнее» (data-testid=risks-list-item-button). Без него карточка
   * пассивна (обратная совместимость с ContractDetailPage/Dashboard).
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
  const recByRiskId = new Map<string, Recommendation>();
  for (const rec of recommendations ?? []) {
    if (rec.risk_id) recByRiskId.set(rec.risk_id, rec);
  }
  const count = risks?.length ?? 0;

  return (
    <section
      aria-label="Ключевые риски"
      className="flex flex-col gap-4 rounded-xl border border-border-subtle bg-bg p-6 shadow-none"
    >
      <header className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-18 font-semibold text-fg">Ключевые риски</h2>
        {count > 0 ? <span className="text-13 text-fg-subtle">{count} найдено</span> : null}
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
              recommendation={risk.id ? recByRiskId.get(risk.id) : undefined}
              {...(onRiskClick ? { onClick: () => onRiskClick(risk) } : {})}
            />
          ))}
        </ul>
      )}
    </section>
  );
}

interface RiskItemProps {
  risk: Risk;
  recommendation?: Recommendation | undefined;
  onClick?: () => void;
}

function RiskItem({ risk, recommendation, onClick }: RiskItemProps): JSX.Element {
  const [showWording, setShowWording] = useState(false);
  const level = risk.level;
  const hasWording = Boolean(recommendation?.recommended_text);

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

      {risk.clause_ref || recommendation?.explanation ? (
        <div className="flex flex-wrap items-center gap-2 text-12">
          {risk.clause_ref ? (
            <span className="font-medium text-fg-subtle">{risk.clause_ref}</span>
          ) : null}
          {risk.clause_ref && recommendation?.explanation ? (
            <span aria-hidden className="text-fg-disabled">
              ·
            </span>
          ) : null}
          {recommendation?.explanation ? (
            <span className="text-brand-600">{recommendation.explanation}</span>
          ) : null}
        </div>
      ) : null}

      {onClick || hasWording ? (
        <div className="flex flex-wrap items-center gap-4 text-13 font-medium">
          {onClick ? (
            <button
              type="button"
              onClick={onClick}
              data-testid="risks-list-item-button"
              className="rounded-sm text-brand-500 hover:underline focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1"
            >
              Подробнее
            </button>
          ) : null}
          {hasWording ? (
            <button
              type="button"
              onClick={() => setShowWording((v) => !v)}
              aria-expanded={showWording}
              className="rounded-sm text-fg-subtle hover:text-fg focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1"
            >
              {showWording ? 'Скрыть формулировку' : 'Показать формулировку'}
            </button>
          ) : null}
        </div>
      ) : null}

      {showWording && recommendation ? (
        <div className="flex flex-col gap-2 rounded-md bg-bg-muted p-3 text-13">
          {recommendation.original_text ? (
            <p className="text-fg-muted line-through">{recommendation.original_text}</p>
          ) : null}
          {recommendation.recommended_text ? (
            <p className="text-fg">{recommendation.recommended_text}</p>
          ) : null}
        </div>
      ) : null}
    </li>
  );
}
