// ComparisonPage (FE-TASK-047) — экран «Сравнение версий» (Figma экран 6, 9 состояний).
//
// URL: /contracts/:id/compare?base=&target=  (§6.1)
// RBAC (§5.5/§5.6): comparison.run = LAWYER + ORG_ADMIN. BUSINESS_USER видит
// inline RoleRestricted-экран (Pattern B), а не /403 redirect — пользователь
// уже на authorized маршруте, мы скрываем содержимое, а не блокируем навигацию.
//
// Источник данных: features/comparison-start (useDiff/useStartComparison).
// 404 DIFF_NOT_FOUND — soft-state «Сравнение ещё не готово» (§9.3 catalog +
// is-diff-not-ready helper). SSE COMPARISON_COMPLETED инвалидирует qk.diff
// (см. useEventStream §7.7) — fresh-fetch произойдёт автоматически.
//
// DiffViewer (widgets/diff-viewer) подгружается через React.lazy → отдельный
// Vite-чанк chunks/diff-viewer (§6.3, ≤150 КБ gzip), включает diff-match-patch
// и Web Worker. Грузим только когда есть готовый diff (после useDiff success).
import { lazy, Suspense, useCallback, useMemo, useState } from 'react';
import { useParams, useSearchParams } from 'react-router-dom';

import {
  isDiffNotReadyError,
  useDiff,
  useStartComparison,
  type VersionDiffResult,
} from '@/features/comparison-start';
import { isOrchestratorError, toUserMessage } from '@/shared/api';
import { useCan } from '@/shared/auth';
import { Button, Spinner, useToast } from '@/shared/ui';
import {
  ChangeCounters,
  type ChangesFilter,
  ChangesTable,
  type ComparisonRisksGroups,
  ComparisonVerdictCard,
  computeChangeCounters,
  computeRiskDelta,
  computeVerdict,
  groupBySection,
  KeyDiffsBySection,
  RiskProfileDelta,
  type RiskProfileSnapshot,
  RisksGroups,
  TabsFilters,
  type VersionMetadata,
  VersionMetaHeader,
} from '@/widgets/version-compare';

// React.lazy → отдельный chunk (Vite manualChunks: chunks/diff-viewer).
// Default-export DiffViewer (см. widgets/diff-viewer/ui/diff-viewer.tsx).
const LazyDiffViewer = lazy(async () => {
  const mod = await import('@/widgets/diff-viewer');
  return { default: mod.DiffViewer };
});

interface ParagraphForDiff {
  id: string;
  baseText: string;
  targetText: string;
  status: 'added' | 'removed' | 'modified' | 'unchanged';
  section?: string;
}

/**
 * Конвертирует VersionDiffResult в массив параграфов для DiffViewer.
 * API не отдаёт «параграфы» напрямую — собираем по path из text_diffs.
 */
function diffsToParagraphs(diff: VersionDiffResult): ParagraphForDiff[] {
  return diff.textDiffs.map((change, idx) => {
    const status: ParagraphForDiff['status'] =
      change.type === 'added' ? 'added' : change.type === 'removed' ? 'removed' : 'modified';
    return {
      id: change.path ?? `diff-${idx}`,
      baseText: change.old_text ?? '',
      targetText: change.new_text ?? '',
      status,
      ...(change.path ? { section: change.path } : {}),
    };
  });
}

// API VersionDiff не содержит risk_profile/risks — агрегация профилей и групп
// рисков пойдёт через GET /risks в будущей итерации (FE-TASK-048). До тех пор
// возвращаем undefined/пустые группы; виджеты корректно показывают плейсхолдеры.
const EMPTY_RISKS_GROUPS: ComparisonRisksGroups = {
  resolved: [],
  introduced: [],
  unchanged: [],
};

interface PageHeaderProps {
  contractId: string | undefined;
  base: string | null;
  target: string | null;
  hasBoth: boolean;
  onRecompute: () => void;
  isRecomputing: boolean;
}

function PageHeader({
  contractId,
  base,
  target,
  hasBoth,
  onRecompute,
  isRecomputing,
}: PageHeaderProps): JSX.Element {
  return (
    <header className="flex flex-col gap-2 md:flex-row md:items-end md:justify-between">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold text-fg">Сравнение версий</h1>
        <p className="text-sm text-fg-muted">
          Договор: {contractId ?? '—'} · База: {base ?? '—'} · Целевая: {target ?? '—'}
        </p>
      </div>
      <div className="flex items-center gap-2">
        <Button
          type="button"
          variant="secondary"
          onClick={onRecompute}
          disabled={!hasBoth || isRecomputing}
          data-testid="recompute-comparison"
        >
          {isRecomputing ? 'Запуск…' : 'Пересчитать'}
        </Button>
      </div>
    </header>
  );
}

function NoVersionsSelected(): JSX.Element {
  return (
    <section
      data-testid="state-no-versions"
      className="flex flex-col items-center justify-center gap-3 rounded-md border border-dashed border-border bg-bg-muted p-12 text-center"
    >
      <h2 className="text-lg font-semibold text-fg">Версии не выбраны</h2>
      <p className="max-w-md text-sm text-fg-muted">
        Передайте параметры запроса <code>?base=…&amp;target=…</code> или вернитесь на карточку
        договора и выберите две версии для сравнения.
      </p>
    </section>
  );
}

function SingleVersionSelected({
  base,
  target,
}: {
  base: string | null;
  target: string | null;
}): JSX.Element {
  return (
    <section
      data-testid="state-single-version"
      className="flex flex-col items-center justify-center gap-3 rounded-md border border-dashed border-border bg-bg-muted p-12 text-center"
    >
      <h2 className="text-lg font-semibold text-fg">Выберите вторую версию</h2>
      <p className="max-w-md text-sm text-fg-muted">
        Указана только одна версия (<code>{base ?? target ?? '—'}</code>). Для сравнения нужны обе:{' '}
        <code>?base=…&amp;target=…</code>.
      </p>
    </section>
  );
}

function LoadingState(): JSX.Element {
  return (
    <section
      data-testid="state-loading"
      aria-busy="true"
      className="flex min-h-[240px] flex-col items-center justify-center gap-3 rounded-md border border-border bg-bg p-8"
    >
      <Spinner size="lg" aria-hidden="true" />
      <p className="text-sm text-fg-muted">Готовим сравнение…</p>
    </section>
  );
}

function NotReadyState({
  onRecompute,
  isRecomputing,
}: {
  onRecompute: () => void;
  isRecomputing: boolean;
}): JSX.Element {
  return (
    <section
      data-testid="state-not-ready"
      className="flex flex-col items-center justify-center gap-3 rounded-md border border-dashed border-border bg-bg-muted p-12 text-center"
    >
      <h2 className="text-lg font-semibold text-fg">Сравнение ещё не готово</h2>
      <p className="max-w-md text-sm text-fg-muted">
        Дождитесь окончания обработки целевой версии. Результат появится автоматически.
      </p>
      <Button
        type="button"
        variant="primary"
        onClick={onRecompute}
        disabled={isRecomputing}
        data-testid="recompute-from-not-ready"
      >
        {isRecomputing ? 'Запуск…' : 'Запустить сравнение'}
      </Button>
    </section>
  );
}

function ErrorState({ message, onRetry }: { message: string; onRetry: () => void }): JSX.Element {
  return (
    <section
      data-testid="state-error"
      role="alert"
      className="flex flex-col items-center justify-center gap-3 rounded-md border border-danger/30 bg-bg p-12 text-center"
    >
      <h2 className="text-lg font-semibold text-danger">Не удалось получить сравнение</h2>
      <p className="max-w-md text-sm text-fg-muted">{message}</p>
      <Button type="button" variant="secondary" onClick={onRetry} data-testid="retry-comparison">
        Повторить
      </Button>
    </section>
  );
}

function NoChangesState(): JSX.Element {
  return (
    <section
      data-testid="state-no-changes"
      className="flex flex-col items-center justify-center gap-3 rounded-md border border-success/30 bg-bg p-12 text-center"
    >
      <h2 className="text-lg font-semibold text-fg">Изменений между версиями нет</h2>
      <p className="max-w-md text-sm text-fg-muted">
        Текстовая и структурная разница равна нулю. Целевая версия идентична базовой.
      </p>
    </section>
  );
}

function RoleRestrictedState(): JSX.Element {
  return (
    <section
      data-testid="state-role-restricted"
      className="flex flex-col items-center justify-center gap-3 rounded-md border border-warning/30 bg-bg p-12 text-center"
    >
      <h2 className="text-lg font-semibold text-fg">Сравнение доступно только юристам</h2>
      <p className="max-w-md text-sm text-fg-muted">
        У вашей роли нет доступа к сравнению версий. Обратитесь к администратору организации или
        попросите коллегу-юриста запустить сравнение.
      </p>
    </section>
  );
}

interface ReadyContentProps {
  diff: VersionDiffResult;
  base: string;
  target: string;
}

function ReadyContent({ diff, base, target }: ReadyContentProps): JSX.Element {
  const [filter, setFilter] = useState<ChangesFilter>('all');

  const counters = useMemo(() => computeChangeCounters(diff), [diff]);
  const sections = useMemo(() => groupBySection(diff), [diff]);
  const profiles = useMemo<{
    base?: RiskProfileSnapshot;
    target?: RiskProfileSnapshot;
  }>(() => ({}), []);
  const verdict = useMemo(
    () => computeVerdict(profiles.base, profiles.target),
    [profiles.base, profiles.target],
  );
  const riskDelta = useMemo(
    () => computeRiskDelta(profiles.base, profiles.target),
    [profiles.base, profiles.target],
  );
  const risks = EMPTY_RISKS_GROUPS;

  const baseMeta: VersionMetadata = useMemo(() => ({ versionId: base }), [base]);
  const targetMeta: VersionMetadata = useMemo(() => ({ versionId: target }), [target]);

  const paragraphs = useMemo(() => diffsToParagraphs(diff), [diff]);
  const tabCounters: Partial<Record<ChangesFilter, number>> = useMemo(
    () => ({
      all: counters.total,
      textual: counters.textual,
      structural: counters.structural,
    }),
    [counters],
  );

  if (counters.total === 0) {
    return (
      <div className="flex flex-col gap-6">
        <VersionMetaHeader base={baseMeta} target={targetMeta} />
        <NoChangesState />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6" data-testid="state-ready">
      <VersionMetaHeader base={baseMeta} target={targetMeta} />

      <ComparisonVerdictCard
        verdict={verdict}
        {...(profiles.base ? { baseProfile: profiles.base } : {})}
        {...(profiles.target ? { targetProfile: profiles.target } : {})}
      />

      <ChangeCounters counters={counters} />

      <RiskProfileDelta
        delta={riskDelta}
        {...(profiles.base ? { baseProfile: profiles.base } : {})}
        {...(profiles.target ? { targetProfile: profiles.target } : {})}
      />

      <KeyDiffsBySection sections={sections} />

      <section
        aria-label="Подробный список изменений"
        className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
      >
        <header className="flex flex-col gap-2">
          <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">Изменения</h2>
          <TabsFilters value={filter} onChange={setFilter} counters={tabCounters} />
        </header>

        <ChangesTable
          changes={diff.textDiffs}
          structuralChanges={diff.structuralDiffs}
          filter={filter}
        />
      </section>

      <RisksGroups groups={risks} />

      <section
        aria-label="Сравнение текста"
        className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm"
      >
        <header>
          <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
            Сравнение текста
          </h2>
          <p className="mt-1 text-xs text-fg-muted">
            Side-by-side: слева — базовая версия, справа — целевая. Inline — единая колонка с
            разметкой добавлений/удалений.
          </p>
        </header>
        <Suspense
          fallback={
            <div
              data-testid="diff-viewer-suspense"
              aria-busy="true"
              className="flex min-h-[240px] items-center justify-center"
            >
              <Spinner size="md" aria-hidden="true" />
            </div>
          }
        >
          <LazyDiffViewer paragraphs={paragraphs} />
        </Suspense>
      </section>
    </div>
  );
}

export function ComparisonPage(): JSX.Element {
  const { id } = useParams<{ id: string }>();
  const [searchParams] = useSearchParams();
  const base = searchParams.get('base');
  const target = searchParams.get('target');

  const canCompare = useCan('comparison.run');
  const toaster = useToast();

  const startMutation = useStartComparison({
    onError: (_err, msg) => {
      toaster.error({ title: msg.title, ...(msg.hint ? { description: msg.hint } : {}) });
    },
  });

  const hasBoth = Boolean(base && target);

  const diffQuery = useDiff(
    {
      contractId: id ?? '',
      baseVersionId: base ?? '',
      targetVersionId: target ?? '',
    },
    { enabled: Boolean(id && hasBoth && canCompare) },
  );

  const recompute = useCallback(() => {
    if (!id || !base || !target) return;
    startMutation.startComparison({
      contractId: id,
      baseVersionId: base,
      targetVersionId: target,
    });
  }, [id, base, target, startMutation]);

  const errorMessage = useMemo(() => {
    if (!diffQuery.error) return '';
    if (isOrchestratorError(diffQuery.error)) {
      return toUserMessage(diffQuery.error).title;
    }
    return 'Произошла непредвиденная ошибка. Повторите попытку.';
  }, [diffQuery.error]);

  return (
    <main
      data-testid="page-comparison"
      className="mx-auto flex min-h-[60vh] max-w-6xl flex-col gap-6 px-6 py-12"
    >
      <PageHeader
        contractId={id}
        base={base}
        target={target}
        hasBoth={hasBoth}
        onRecompute={recompute}
        isRecomputing={startMutation.isPending}
      />

      {!canCompare ? (
        <RoleRestrictedState />
      ) : !base && !target ? (
        <NoVersionsSelected />
      ) : !hasBoth ? (
        <SingleVersionSelected base={base} target={target} />
      ) : diffQuery.isLoading ? (
        <LoadingState />
      ) : diffQuery.error && isDiffNotReadyError(diffQuery.error) ? (
        <NotReadyState onRecompute={recompute} isRecomputing={startMutation.isPending} />
      ) : diffQuery.error ? (
        <ErrorState message={errorMessage} onRetry={() => void diffQuery.refetch()} />
      ) : diffQuery.data ? (
        <ReadyContent diff={diffQuery.data} base={base ?? ''} target={target ?? ''} />
      ) : (
        <LoadingState />
      )}
    </main>
  );
}
