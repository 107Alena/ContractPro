// RiskProfileCard — виджет «Профиль рисков» (§16.5, §17.5 row 9+13).
// Источник данных — AnalysisResults.risk_profile + aggregate_score (GET
// /results). Потребители: ResultPage (FE-TASK-046) и ContractDetail
// KeyRisks (FE-TASK-048). Role-agnostic: скрытие для BUSINESS_USER —
// на уровне page через <Can I="risks.view">.
import type { RiskLevel } from '@/entities/risk';
import { RISK_LEVEL_META } from '@/entities/risk';
import type { components } from '@/shared/api/openapi';
import { Badge } from '@/shared/ui/badge';
import { Spinner } from '@/shared/ui/spinner';

type RiskProfile = components['schemas']['RiskProfile'];
type AggregateScore = components['schemas']['AggregateScore'];

export interface RiskProfileCardProps {
  profile?: RiskProfile | undefined;
  aggregate?: AggregateScore | undefined;
  isLoading?: boolean | undefined;
  error?: unknown;
}

export function RiskProfileCard({
  profile,
  aggregate,
  isLoading,
  error,
}: RiskProfileCardProps): JSX.Element {
  return (
    <section
      aria-label="Профиль рисков"
      data-testid="risk-profile-card"
      className="flex flex-col gap-4 rounded-md border border-border bg-bg p-5 shadow-sm"
    >
      <header className="flex flex-col gap-1">
        <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
          Профиль рисков
        </h2>
        <p className="text-xs text-fg-muted">Агрегированная оценка по выявленным рискам</p>
      </header>

      {isLoading && !profile ? (
        <div
          data-testid="risk-profile-loading"
          className="flex min-h-[120px] items-center justify-center"
          aria-busy="true"
        >
          <Spinner size="md" aria-hidden="true" />
        </div>
      ) : error ? (
        <p role="alert" className="text-sm text-danger">
          Не удалось загрузить профиль рисков.
        </p>
      ) : !profile ? (
        <p className="text-sm text-fg-muted" data-testid="risk-profile-empty">
          Профиль рисков появится после анализа версии.
        </p>
      ) : (
        <>
          <AggregateVerdict profile={profile} aggregate={aggregate} />
          <Counters profile={profile} />
        </>
      )}
    </section>
  );
}

function AggregateVerdict({
  profile,
  aggregate,
}: {
  profile: RiskProfile;
  aggregate?: AggregateScore | undefined;
}): JSX.Element {
  const overall = profile.overall_level;
  const meta = overall ? RISK_LEVEL_META[overall as RiskLevel] : undefined;
  const scoreLabel = aggregate?.label ?? meta?.label ?? 'Профиль рисков';
  const scoreValue = typeof aggregate?.score === 'number' ? aggregate.score.toFixed(2) : null;

  return (
    <div
      className="flex flex-col gap-2 rounded-md border border-border bg-bg-muted p-4 sm:flex-row sm:items-center sm:justify-between"
      data-testid="risk-profile-verdict"
    >
      <div className="flex flex-col gap-1">
        <p className="text-xs font-medium uppercase tracking-wide text-fg-muted">Итоговая оценка</p>
        <p className="text-lg font-semibold text-fg" data-testid="risk-profile-label">
          {scoreLabel}
        </p>
      </div>
      <div className="flex items-center gap-2">
        {meta ? (
          <Badge variant={meta.tone} data-testid="risk-profile-tone">
            {meta.label}
          </Badge>
        ) : null}
        {scoreValue ? (
          <span className="text-sm text-fg-muted" data-testid="risk-profile-score">
            {scoreValue}
          </span>
        ) : null}
      </div>
    </div>
  );
}

function Counters({ profile }: { profile: RiskProfile }): JSX.Element {
  const high = profile.high_count ?? 0;
  const medium = profile.medium_count ?? 0;
  const low = profile.low_count ?? 0;
  return (
    <dl
      className="grid grid-cols-3 gap-3"
      data-testid="risk-profile-counters"
      aria-label="Распределение рисков"
    >
      <CounterItem level="high" value={high} />
      <CounterItem level="medium" value={medium} />
      <CounterItem level="low" value={low} />
    </dl>
  );
}

function CounterItem({ level, value }: { level: RiskLevel; value: number }): JSX.Element {
  const meta = RISK_LEVEL_META[level];
  return (
    <div
      className="flex flex-col items-start gap-1 rounded-md border border-border bg-bg p-3"
      data-testid={`risk-profile-counter-${level}`}
    >
      <dt className="text-xs uppercase tracking-wide text-fg-muted">{meta.label}</dt>
      <dd className="text-2xl font-semibold text-fg">{value}</dd>
    </div>
  );
}
