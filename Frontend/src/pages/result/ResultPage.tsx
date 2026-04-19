// ResultPage (FE-TASK-046) — экран «Результат проверки» (Figma 5 — 8 состояний,
// §17.4). URL: /contracts/:id/versions/:vid/result (auth, §6.1 / §17.1).
//
// Источник данных (§17.5):
//   - useContract(id) → GET /contracts/{id}. Нужен ради processing_status
//     текущей версии (state machine) и заголовка.
//   - useResults({contractId,versionId}) → GET /results. Агрегированный
//     AnalysisResults с risks/recommendations/summary/key_parameters/...
//     Backend фильтрует по роли: BUSINESS_USER получает только
//     summary/aggregate_score/key_parameters. Поэтому второй запрос
//     (/risks, /summary, /recommendations) не требуется — hook'и
//     entities/result существуют для других страниц (Dashboard).
//
// State-machine (8 состояний §17.4, precedence сверху вниз):
//   1. contract.isLoading → LoadingState
//   2. CONTRACT/VERSION_NOT_FOUND или отсутствие version → NotFoundState (8)
//   3. version.processing_status = REJECTED → RejectedState (7)
//   4. processing_status = FAILED → FailedState (6) + RecheckButton
//   5. processing_status = AWAITING_USER_INPUT → AwaitingInputState (5),
//      CTA ведёт на /contracts/{id} где живёт LowConfidenceConfirmProvider
//   6. processing_status ∈ {UPLOADED, QUEUED, PROCESSING, ANALYZING,
//      GENERATING_REPORTS} → ProcessingBanner (3)
//   7. processing_status ∈ {READY, PARTIALLY_FAILED} && results.data:
//        - PARTIALLY_FAILED → ReadyContent + WarningsBanner (4)
//        - READY + LAWYER/ORG_ADMIN → ReadyContent (1)
//        - READY + BUSINESS_USER → ReadyContent с <Can>-скрытыми
//          секциями risks/recommendations/deviations/risk-profile (2)
//
// RBAC (§5.6 Pattern B):
//   - Единый <Can>-wrapper вокруг risks.view / recommendations.view секций.
//     `useResults` единственный запрос — не блокируем `enabled`: backend
//     уже фильтрует поля по роли, UI дополнительно прячет секции.
//
// RiskDetailsDrawer:
//   - state (selectedRiskId: string|null) живёт page-local (паттерн
//     ContractDetailPage isPDFOpen). RisksList получает onRiskClick,
//     drawer контролируется open={selectedRiskId!==null}.
//
// useEventStream:
//   - Подписка на SSE status_update (§7.7). Обновляет qk.contracts.status,
//     но byId/results не инвалидирует (как и ContractDetailPage). READY →
//     пользователь видит актуальный статус, refetch по staleTime 30s +
//     kнопка «Проверить заново» вручную триггерят свежие данные.
import { useCallback, useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';

import { type ContractDetails, useContract } from '@/entities/contract';
import { type AnalysisResults, useResults } from '@/entities/result';
import { RiskDetailsDrawer } from '@/entities/risk';
import { isOrchestratorError, toUserMessage, useEventStream } from '@/shared/api';
import { Can } from '@/shared/auth';
import { Button } from '@/shared/ui/button';
import { FeedbackBlock } from '@/widgets/feedback-block';
import { LegalDisclaimer } from '@/widgets/legal-disclaimer';
import { MandatoryConditionsChecklist } from '@/widgets/mandatory-conditions-checklist';
import { RecommendationsList } from '@/widgets/recommendations-list';
import { RiskProfileCard } from '@/widgets/risk-profile-card';
import { RisksList } from '@/widgets/risks-list';

import { DeviationsFromPolicy } from './ui/deviations-from-policy';
import { DocumentCard } from './ui/document-card';
import { ExportShareButton } from './ui/export-share-button';
import { NextActions } from './ui/next-actions';
import { ProcessingBanner } from './ui/processing-banner';
import { RecheckButton } from './ui/recheck-button';
import {
  AwaitingInputState,
  ErrorState,
  FailedState,
  LoadingState,
  NotFoundState,
  RejectedState,
} from './ui/state-screens';
import { SummaryTable } from './ui/summary-table';
import { WarningsBanner } from './ui/warnings-banner';

type ProcessingStatus = NonNullable<ContractDetails['current_version']>['processing_status'];

const PROCESSING_STATUSES = new Set<ProcessingStatus>([
  'UPLOADED',
  'QUEUED',
  'PROCESSING',
  'ANALYZING',
  'GENERATING_REPORTS',
]);

function isRecordNotFound(err: unknown): boolean {
  return (
    isOrchestratorError(err) &&
    (err.error_code === 'CONTRACT_NOT_FOUND' || err.error_code === 'VERSION_NOT_FOUND')
  );
}

interface ReadyContentProps {
  contract: ContractDetails;
  results: AnalysisResults;
  contractId: string;
  versionId: string;
  showWarningsBanner: boolean;
  warningMessage: string | undefined;
}

function ReadyContent({
  contract,
  results,
  contractId,
  versionId,
  showWarningsBanner,
  warningMessage,
}: ReadyContentProps): JSX.Element {
  const [selectedRiskId, setSelectedRiskId] = useState<string | null>(null);

  const openRisk = useCallback((risk: { id?: string }) => {
    setSelectedRiskId(risk.id ?? null);
  }, []);

  const closeRisk = useCallback((open: boolean) => {
    if (!open) setSelectedRiskId(null);
  }, []);

  const selectedRisk = useMemo(
    () => (results.risks ?? []).find((risk) => risk.id === selectedRiskId) ?? null,
    [results.risks, selectedRiskId],
  );

  return (
    <div className="flex flex-col gap-6" data-testid="state-ready">
      <DocumentCard contract={contract} results={results} />

      {showWarningsBanner ? <WarningsBanner message={warningMessage} /> : null}

      <SummaryTable results={results} />

      <Can I="risks.view">
        <RiskProfileCard
          {...(results.risk_profile !== undefined ? { profile: results.risk_profile } : {})}
          {...(results.aggregate_score !== undefined ? { aggregate: results.aggregate_score } : {})}
        />
      </Can>

      <Can I="risks.view">
        <MandatoryConditionsChecklist />
      </Can>

      <Can I="risks.view">
        <RisksList risks={results.risks ?? []} onRiskClick={openRisk} />
      </Can>

      <Can I="recommendations.view">
        <RecommendationsList items={results.recommendations ?? []} />
      </Can>

      <Can I="risks.view">
        <DeviationsFromPolicy risks={results.risks ?? []} />
      </Can>

      <NextActions results={results} />

      <FeedbackBlock contractId={contractId} versionId={versionId} />

      <LegalDisclaimer />

      <RiskDetailsDrawer
        open={selectedRiskId !== null}
        onOpenChange={closeRisk}
        risk={selectedRisk}
      />
    </div>
  );
}

interface PageHeaderProps {
  contract: ContractDetails | undefined;
  contractId: string;
  versionId: string;
  disableActions: boolean;
}

function PageHeader({
  contract,
  contractId,
  versionId,
  disableActions,
}: PageHeaderProps): JSX.Element {
  const title = contract?.title ?? 'Результат проверки';
  const versionLabel = contract?.current_version?.version_number
    ? `v${contract.current_version.version_number}`
    : versionId;

  return (
    <header className="flex flex-col gap-2 md:flex-row md:items-end md:justify-between">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold text-fg">{title}</h1>
        <p className="text-sm text-fg-muted">Версия: {versionLabel}</p>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <RecheckButton
          contractId={contractId}
          versionId={versionId}
          disabled={disableActions}
          variant="secondary"
        />
        <ExportShareButton
          contractId={contractId}
          versionId={versionId}
          disabled={disableActions}
        />
      </div>
    </header>
  );
}

export function ResultPage(): JSX.Element {
  const { id, vid } = useParams<{ id: string; vid: string }>();
  const contractId = id ?? '';
  const versionId = vid ?? '';

  const contractQuery = useContract(contractId || undefined);
  const resultsQuery = useResults({ contractId, versionId });

  useEventStream(contractId || undefined);

  const retryAll = useCallback(() => {
    void contractQuery.refetch();
    void resultsQuery.refetch();
  }, [contractQuery, resultsQuery]);

  const contract = contractQuery.data;
  const version = contract?.current_version;
  const processingStatus = version?.processing_status;

  // Заголовочные кнопки «Проверить заново» / «Скачать отчёт» имеют смысл
  // только для окончательных статусов — в промежуточных рендерим placeholder
  // disabled, чтобы layout не прыгал между состояниями.
  const disableHeaderActions =
    !processingStatus ||
    processingStatus === 'REJECTED' ||
    processingStatus === 'UPLOADED' ||
    processingStatus === 'QUEUED';

  const content = ((): JSX.Element => {
    if (contractQuery.isLoading || (!contract && !contractQuery.error)) {
      return <LoadingState />;
    }

    if (isRecordNotFound(contractQuery.error) || !contract) {
      return <NotFoundState contractId={contractId || undefined} />;
    }

    if (contractQuery.error) {
      const msg = isOrchestratorError(contractQuery.error)
        ? toUserMessage(contractQuery.error).title
        : 'Произошла непредвиденная ошибка. Повторите попытку.';
      return (
        <ErrorState
          message={msg}
          actions={
            <Button
              type="button"
              variant="secondary"
              onClick={retryAll}
              data-testid="retry-results"
            >
              Повторить
            </Button>
          }
        />
      );
    }

    if (!version) {
      return <NotFoundState contractId={contractId} />;
    }

    if (processingStatus === 'REJECTED') {
      return (
        <RejectedState
          contractId={contractId}
          message={version.processing_status_message ?? undefined}
        />
      );
    }

    if (processingStatus === 'FAILED') {
      return (
        <FailedState
          message={version.processing_status_message ?? undefined}
          recheckButton={
            <RecheckButton contractId={contractId} versionId={versionId} variant="primary" />
          }
        />
      );
    }

    if (processingStatus === 'AWAITING_USER_INPUT') {
      return (
        <AwaitingInputState
          contractId={contractId}
          message={version.processing_status_message ?? undefined}
        />
      );
    }

    if (processingStatus && PROCESSING_STATUSES.has(processingStatus)) {
      return (
        <ProcessingBanner
          status={processingStatus}
          contractId={contractId}
          message={version.processing_status_message ?? undefined}
        />
      );
    }

    // READY / PARTIALLY_FAILED
    if (resultsQuery.isLoading) {
      return <LoadingState />;
    }

    if (isRecordNotFound(resultsQuery.error)) {
      return <NotFoundState contractId={contractId} />;
    }

    if (resultsQuery.error) {
      const msg = isOrchestratorError(resultsQuery.error)
        ? toUserMessage(resultsQuery.error).title
        : 'Не удалось загрузить результаты анализа.';
      return (
        <ErrorState
          message={msg}
          actions={
            <Button
              type="button"
              variant="secondary"
              onClick={retryAll}
              data-testid="retry-results"
            >
              Повторить
            </Button>
          }
        />
      );
    }

    if (!resultsQuery.data) {
      return <LoadingState />;
    }

    return (
      <ReadyContent
        contract={contract}
        results={resultsQuery.data}
        contractId={contractId}
        versionId={versionId}
        showWarningsBanner={processingStatus === 'PARTIALLY_FAILED'}
        warningMessage={version.processing_status_message ?? undefined}
      />
    );
  })();

  return (
    <main
      data-testid="page-result"
      className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-4 py-6 md:px-6 md:py-8"
    >
      <PageHeader
        contract={contract}
        contractId={contractId}
        versionId={versionId}
        disableActions={disableHeaderActions}
      />
      {content}
    </main>
  );
}
