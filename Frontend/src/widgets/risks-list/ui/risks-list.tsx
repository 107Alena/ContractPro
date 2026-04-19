// RisksList — список рисков версии (экран 8 Figma «Ключевые риски»; §17.4,
// §17.5 artifact RISK_ANALYSIS). В v1 FE-TASK-045 виджет сам не тянет /risks
// (это FE-TASK-046/048) — принимает `risks` пропом; при отсутствии рисует
// empty state «Появятся после анализа».
//
// RBAC: виджет скрывается на уровне page через <Can I="risks.view">
// (Pattern B из §5.6.1). Сам компонент role-agnostic.
//
// TODO(FE-TASK-046/048): при подключении useRisks(contractId, versionId) в
// parent-page сам запрос должен быть защищён `enabled: useCan('risks.view')` —
// §5.6.1 требует, чтобы скрытые для роли данные не загружались. Не переносить
// useQuery внутрь этого виджета без такого guard'а.
import { RiskBadge } from '@/entities/risk';
import type { components } from '@/shared/api/openapi';
import { Spinner } from '@/shared/ui';

type Risk = components['schemas']['Risk'];

export interface RisksListProps {
  risks?: readonly Risk[] | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
  /**
   * Открывает RiskDetailsDrawer (§16.5 компонентное дерево ResultPage,
   * §17.5 artifact RISK_ANALYSIS). Когда передан — каждый элемент списка
   * рендерится как `<button>`, кликабелен и получает роль кнопки. Когда
   * пропущен — элементы остаются пассивными `<li>` (обратная совместимость
   * с ContractDetailPage/Dashboard, где drawer пока не подключён).
   */
  onRiskClick?: ((risk: Risk) => void) | undefined;
}

export function RisksList({ risks, isLoading, error, onRiskClick }: RisksListProps): JSX.Element {
  return (
    <section
      aria-label="Ключевые риски"
      className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header>
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Ключевые риски
        </h2>
        <p className="mt-1 text-xs text-fg-muted">Выявленные юридические и финансовые риски</p>
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
        <p role="alert" className="text-sm text-danger">
          Не удалось загрузить список рисков.
        </p>
      ) : !risks || risks.length === 0 ? (
        <p className="text-sm text-fg-muted" data-testid="risks-list-empty">
          Риски появятся после завершения анализа текущей версии.
        </p>
      ) : (
        <ul className="flex flex-col gap-3" data-testid="risks-list">
          {risks.map((risk, idx) => (
            <RiskItem
              key={risk.id ?? `risk-${idx}`}
              risk={risk}
              {...(onRiskClick ? { onClick: () => onRiskClick(risk) } : {})}
            />
          ))}
        </ul>
      )}
    </section>
  );
}

function RiskItem({ risk, onClick }: { risk: Risk; onClick?: () => void }): JSX.Element {
  const body = (
    <>
      <div className="flex flex-wrap items-baseline gap-2">
        {risk.level ? <RiskBadge level={risk.level} /> : null}
        {risk.clause_ref ? (
          <span className="text-xs text-fg-muted">Пункт: {risk.clause_ref}</span>
        ) : null}
      </div>
      {risk.description ? <p className="text-sm text-fg">{risk.description}</p> : null}
      {risk.legal_basis ? (
        <p className="text-xs text-fg-muted">Основание: {risk.legal_basis}</p>
      ) : null}
    </>
  );

  if (onClick) {
    return (
      <li data-testid="risks-list-item">
        <button
          type="button"
          onClick={onClick}
          data-testid="risks-list-item-button"
          className="flex w-full flex-col gap-2 rounded-md border border-border bg-bg-muted p-3 text-left transition hover:bg-bg-muted/70 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500"
        >
          {body}
        </button>
      </li>
    );
  }

  return (
    <li
      data-testid="risks-list-item"
      className="flex flex-col gap-2 rounded-md border border-border bg-bg-muted p-3"
    >
      {body}
    </li>
  );
}
