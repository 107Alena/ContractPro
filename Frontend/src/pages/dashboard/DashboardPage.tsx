// DashboardPage (FE-TASK-042) — главный экран после логина.
// Figma-alignment этап 4.3 (Figma 84:2): WelcomeBlock → «Что важно сейчас» →
// двухколоночный layout (776/340) → «Ключевые риски» → TrustFooter.
//
// Архитектура: §17.1 (auth, GET /users/me + GET /contracts?size=5 + SSE),
// §17.4 (widgets), §9.3 (error-state), §5.6 (RBAC guards).
//
// 4 состояния (AC FE-TASK-042): Default / Loading (spinner в виджетах) /
// Empty (/contracts пуст → empty-варианты) / Error (виджеты в error-варианте).
//
// SSE: useEventStream без document_id подписывается на глобальный feed (§7.7);
// status-update попадает в qk.contracts.status(id,vid); /contracts сам по себе
// не инвалидируется (snapshot-цельность), но refetch по staleTime актуализирует.
import { useContracts } from '@/entities/contract';
import { useMe } from '@/entities/user';
import { useEventStream } from '@/shared/api';
import { Can } from '@/shared/auth';
import { BusinessSummary } from '@/widgets/dashboard-business-summary';
import { CurrentActions } from '@/widgets/dashboard-current-actions';
import { KeyRisksCards } from '@/widgets/dashboard-key-risks';
import { LastCheckCard } from '@/widgets/dashboard-last-check';
import { OrgCard } from '@/widgets/dashboard-org-card';
import { ProcessingStatus } from '@/widgets/dashboard-processing-status';
import { QuickStart } from '@/widgets/dashboard-quick-start';
import { RecentChecksTable } from '@/widgets/dashboard-recent-checks';
import { TrustFooter } from '@/widgets/dashboard-trust-footer';
import { WelcomeBlock } from '@/widgets/dashboard-welcome';

const CONTRACTS_PARAMS = { size: 5 } as const;

export function DashboardPage(): JSX.Element {
  const meQuery = useMe();
  const contractsQuery = useContracts(CONTRACTS_PARAMS);

  // Global SSE feed (без documentId) — обновления статусов попадают в
  // qk.contracts.status(...) и будут подхвачены при переходе на detail-page.
  useEventStream(undefined);

  const items = contractsQuery.data?.items ?? [];
  const total = contractsQuery.data?.total;
  const latestContract = items[0];
  const isLoading = contractsQuery.isLoading;
  const error = contractsQuery.error ?? undefined;

  return (
    <div
      data-testid="page-dashboard"
      className="mx-auto flex w-full max-w-7xl flex-col gap-6 px-4 py-6 md:px-8 md:py-8"
    >
      <WelcomeBlock user={meQuery.data} />

      <CurrentActions items={items} isLoading={isLoading} error={error} />

      {/* Двухколоночный layout 776/340 (Figma 89:2). Слева — последняя проверка
          и недавние проверки; справа — быстрый старт и карточка организации. */}
      <div className="grid grid-cols-1 gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
        <div className="flex min-w-0 flex-col gap-5">
          <LastCheckCard contract={latestContract} isLoading={isLoading} error={error} />
          <RecentChecksTable items={items} isLoading={isLoading} error={error} />
        </div>
        <div className="flex flex-col gap-5">
          <Can I="contract.upload">
            <QuickStart />
          </Can>
          <BusinessSummary total={total ?? undefined} isLoading={isLoading} error={error} />
          <ProcessingStatus items={items} isLoading={isLoading} error={error} />
          <OrgCard
            user={meQuery.data}
            isLoading={meQuery.isLoading}
            error={meQuery.error ?? undefined}
          />
        </div>
      </div>

      <Can I="risks.view">
        <KeyRisksCards items={items} isLoading={isLoading} error={error} />
      </Can>

      <TrustFooter />
    </div>
  );
}
