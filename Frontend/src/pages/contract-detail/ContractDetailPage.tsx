// ContractDetailPage (FE-TASK-045) — экран «Карточка документа» (Figma 8,
// 9 + 1 tablet состояний, §17.1/§17.4). URL: /contracts/:id (auth).
//
// Источники данных:
//   - `useContract(id)` → GET /contracts/{id} (entities/contract/api)
//   - `useVersions(id)` → GET /contracts/{id}/versions (entities/version/api)
// Loaders (`ensureQueryData`) из §6.2 в v1 не подключаем — актуально, когда
// страница мигрирует на route-level data-router (отдельная задача в
// follow-ups). useQuery с Suspense:false в текущем проекте — стандартная
// форма (как DashboardPage/ComparisonPage).
//
// RBAC Pattern B (§5.6.1): KeyRisks/Recommendations/DeviationsChecklist
// скрываются inline через <Can I="risks.view" | "recommendations.view">;
// BUSINESS_USER остаётся на странице и видит остальные блоки. В v1 сами
// хуки useRisks/useRecommendations ещё не созданы (scope FE-TASK-046/048) —
// виджеты принимают пустые props и рендерят empty-state.
//
// 404 CONTRACT_NOT_FOUND → inline «NotFoundState» вместо redirect /404 —
// URL сохраняется, пользователь может исправить опечатку (см. §9.3 catalog).
//
// PDFNavigator: отдельный chunk `chunks/pdf-preview` (vite.config manualChunks).
// React.lazy + динамический import загружает chunk только после клика на
// тумблер «Показать PDF» — удовлетворяет AC «lazy-загружается при тумблере».
// В v1 реальный pdfjs-dist не установлен, виджет — stub со статичной заглушкой.
import { lazy, Suspense, useCallback, useState } from 'react';
import { Link, useParams } from 'react-router-dom';

import { type ContractDetails, useContract } from '@/entities/contract';
import { useVersions, type VersionDetails } from '@/entities/version';
import { isOrchestratorError, toUserMessage, useEventStream } from '@/shared/api';
import { Can } from '@/shared/auth';
import { Button, buttonVariants, Spinner } from '@/shared/ui';
import { RecommendationsList } from '@/widgets/recommendations-list';
import { RisksList } from '@/widgets/risks-list';
import { ChecksHistory, VersionsTimeline } from '@/widgets/versions-timeline';

import { DeviationsChecklist } from './ui/deviations-checklist';
import { DocumentHeader } from './ui/document-header';
import { LastCheck } from './ui/last-check';
import { QuickStart } from './ui/quick-start';
import { ReportsShared } from './ui/reports-shared';
import { SummaryCard } from './ui/summary-card';
import { VersionPicker } from './ui/version-picker';

// React.lazy → отдельный chunk `chunks/pdf-preview` (vite.config manualChunks).
// Default-export PDFNavigator загружается динамически только при включении тумблера.
const LazyPDFNavigator = lazy(() => import('@/widgets/pdf-navigator/ui/pdf-navigator'));

function isContractNotFound(err: unknown): boolean {
  return isOrchestratorError(err) && err.error_code === 'CONTRACT_NOT_FOUND';
}

function LoadingState(): JSX.Element {
  return (
    <section
      data-testid="state-loading"
      aria-busy="true"
      className="flex min-h-[240px] flex-col items-center justify-center gap-3 rounded-md border border-border bg-bg p-8"
    >
      <Spinner size="lg" aria-hidden="true" />
      <p className="text-sm text-fg-muted">Загружаем карточку договора…</p>
    </section>
  );
}

function NotFoundState(): JSX.Element {
  return (
    <section
      data-testid="state-not-found"
      className="flex flex-col items-center justify-center gap-3 rounded-md border border-dashed border-border bg-bg-muted p-12 text-center"
    >
      <h2 className="text-lg font-semibold text-fg">Договор не найден</h2>
      <p className="max-w-md text-sm text-fg-muted">
        Возможно, ссылка устарела или договор был удалён. Проверьте URL или вернитесь к списку.
      </p>
      <Link to="/contracts" className={buttonVariants({ variant: 'primary', size: 'md' })}>
        К списку договоров
      </Link>
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
      <h2 className="text-lg font-semibold text-danger">Не удалось загрузить карточку</h2>
      <p className="max-w-md text-sm text-fg-muted">{message}</p>
      <Button type="button" variant="secondary" onClick={onRetry} data-testid="retry-contract">
        Повторить
      </Button>
    </section>
  );
}

interface ReadyContentProps {
  contract: ContractDetails;
  versions: readonly VersionDetails[];
  versionsLoading: boolean;
  versionsError: unknown;
}

function ReadyContent({
  contract,
  versions,
  versionsLoading,
  versionsError,
}: ReadyContentProps): JSX.Element {
  const contractId = contract.contract_id ?? '';
  const currentVersion = contract.current_version;
  const [isPDFOpen, setPDFOpen] = useState(false);

  const togglePDF = useCallback(() => {
    setPDFOpen((prev) => !prev);
  }, []);

  return (
    <div className="flex flex-col gap-6" data-testid="state-ready">
      <DocumentHeader contract={contract} />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <div className="md:col-span-2">
          <SummaryCard contract={contract} />
        </div>
        <LastCheck contract={contract} />
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <QuickStart contractId={contractId} />
        <Can I="risks.view">
          <div className="md:col-span-2">
            <RisksList />
          </div>
        </Can>
      </div>

      <Can I="recommendations.view">
        <RecommendationsList />
      </Can>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <div className="md:col-span-2">
          <VersionsTimeline
            contractId={contractId}
            versions={versions}
            isLoading={versionsLoading}
            error={versionsError ?? undefined}
          />
        </div>
        {currentVersion?.version_id ? (
          <VersionPicker
            contractId={contractId}
            versions={versions}
            selectedVersionId={currentVersion.version_id}
          />
        ) : (
          <VersionPicker contractId={contractId} versions={versions} />
        )}
      </div>

      <ChecksHistory
        contractId={contractId}
        versions={versions}
        isLoading={versionsLoading}
        error={versionsError ?? undefined}
      />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <ReportsShared />
        <Can I="risks.view">
          <DeviationsChecklist />
        </Can>
      </div>

      <div className="flex flex-col gap-3 rounded-md border border-border bg-bg p-5 shadow-sm">
        <header className="flex items-center justify-between gap-2">
          <div>
            <h2 className="text-sm font-semibold uppercase tracking-wide text-fg-muted">
              Превью PDF
            </h2>
            <p className="mt-1 text-xs text-fg-muted">
              Подгружается отдельным чанком только при необходимости.
            </p>
          </div>
          <Button
            type="button"
            variant={isPDFOpen ? 'ghost' : 'secondary'}
            onClick={togglePDF}
            data-testid="pdf-navigator-toggle"
            disabled={!currentVersion?.version_id}
            aria-expanded={isPDFOpen}
            aria-controls="pdf-preview-panel"
          >
            {isPDFOpen ? 'Скрыть PDF' : 'Показать PDF'}
          </Button>
        </header>

        <div id="pdf-preview-panel">
          {isPDFOpen && currentVersion?.version_id ? (
            <Suspense
              fallback={
                <div
                  data-testid="pdf-navigator-suspense"
                  aria-busy="true"
                  className="flex min-h-[240px] items-center justify-center"
                >
                  <Spinner size="md" aria-hidden="true" />
                </div>
              }
            >
              <LazyPDFNavigator
                versionId={currentVersion.version_id}
                sourceFileName={currentVersion.source_file_name ?? undefined}
                onClose={togglePDF}
              />
            </Suspense>
          ) : null}
        </div>
      </div>
    </div>
  );
}

export function ContractDetailPage(): JSX.Element {
  const { id } = useParams<{ id: string }>();
  const contractQuery = useContract(id);
  const versionsQuery = useVersions(id);

  // SSE-подписка на обновления статусов версий текущего договора (§7.7).
  // В useEventStream статус-события попадают в qk.contracts.status(id,vid),
  // но VersionsTimeline/LastCheck читают processing_status из byId/versions
  // снимков. refetch по staleTime (30 s) + ручной retry покрывает типичное
  // ожидание. Полный SSE-refetch для byId/versions — TODO в FE-TASK-048.
  useEventStream(id);

  const retryAll = useCallback(() => {
    void contractQuery.refetch();
    void versionsQuery.refetch();
  }, [contractQuery, versionsQuery]);

  const errorMessage = ((): string => {
    if (!contractQuery.error) return '';
    if (isOrchestratorError(contractQuery.error)) {
      return toUserMessage(contractQuery.error).title;
    }
    return 'Произошла непредвиденная ошибка. Повторите попытку.';
  })();

  return (
    <main
      data-testid="page-contract-detail"
      className="mx-auto flex w-full max-w-7xl flex-col gap-6 px-4 py-6 md:px-6 md:py-8"
    >
      {contractQuery.isLoading ? (
        <LoadingState />
      ) : isContractNotFound(contractQuery.error) ? (
        <NotFoundState />
      ) : contractQuery.error ? (
        <ErrorState message={errorMessage} onRetry={retryAll} />
      ) : contractQuery.data ? (
        <ReadyContent
          contract={contractQuery.data}
          versions={versionsQuery.data?.items ?? []}
          versionsLoading={versionsQuery.isLoading}
          versionsError={versionsQuery.error ?? undefined}
        />
      ) : (
        <LoadingState />
      )}
    </main>
  );
}
