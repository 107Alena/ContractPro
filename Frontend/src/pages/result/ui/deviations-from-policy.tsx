// DeviationsFromPolicy — секция «Отклонения от политики организации»
// (§16.5, §17.5 artifact RECOMMENDATIONS + ORG_POLICY). Показывает только
// риски со ссылкой на `legal_basis`, содержащей «политика» / «policy» —
// прокси-эвристика до появления явного поля `source: 'policy'` в API (backlog).
//
// Гейтинг — на уровне page через <Can I="risks.view">, BUSINESS_USER
// секцию не увидит.
import { isPolicyDeviation, RiskBadge } from '@/entities/risk';
import type { components } from '@/shared/api/openapi';

type Risk = components['schemas']['Risk'];

export interface DeviationsFromPolicyProps {
  risks?: readonly Risk[] | undefined;
}

export function DeviationsFromPolicy({ risks }: DeviationsFromPolicyProps): JSX.Element {
  const deviations = (risks ?? []).filter(isPolicyDeviation);

  return (
    <section
      aria-label="Отклонения от политики"
      data-testid="deviations-from-policy"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header className="flex flex-col gap-1">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Отклонения от политики
        </h2>
        <p className="text-xs text-fg-muted">
          Пункты договора, нарушающие внутренние политики организации
        </p>
      </header>

      {deviations.length === 0 ? (
        <p className="text-sm text-fg-muted" data-testid="deviations-empty">
          Отклонений от внутренних политик не выявлено.
        </p>
      ) : (
        <ul className="flex flex-col gap-2" data-testid="deviations-list">
          {deviations.map((risk, idx) => (
            <li
              key={risk.id ?? `deviation-${idx}`}
              data-testid="deviations-item"
              className="flex flex-col gap-1 rounded-md border border-border bg-bg-muted p-3"
            >
              <div className="flex flex-wrap items-baseline gap-2">
                {risk.level ? <RiskBadge level={risk.level} /> : null}
                {risk.clause_ref ? (
                  <span className="text-xs text-fg-muted">Пункт: {risk.clause_ref}</span>
                ) : null}
              </div>
              {risk.description ? <p className="text-sm text-fg">{risk.description}</p> : null}
              {risk.legal_basis ? (
                <p className="text-xs text-fg-muted">Политика: {risk.legal_basis}</p>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
