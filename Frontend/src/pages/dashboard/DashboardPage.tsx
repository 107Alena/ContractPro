// DashboardPage (FE-TASK-042) — главный экран после логина.
// Упрощённый layout: WelcomeBlock (один CTA «Новая проверка договора») →
// двухколоночный layout (776/340: недавние проверки | быстрый старт + сводка +
// организация) → TrustFooter. Блоки «Что важно сейчас», «Последняя проверка»,
// «Статус обработки» и «Ключевые риски» убраны по продуктовому решению.
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
import { OrgCard } from '@/widgets/dashboard-org-card';
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
  const isLoading = contractsQuery.isLoading;
  const error = contractsQuery.error ?? undefined;

  return (
    <div
      data-testid="page-dashboard"
      className="mx-auto flex w-full max-w-7xl flex-col gap-6 px-4 py-6 md:px-8 md:py-8"
    >
      <WelcomeBlock user={meQuery.data} />

      {/* Двухколоночный layout 776/340 (Figma 89:2). Слева — недавние проверки;
          справа — быстрый старт, сводка и карточка организации. */}
      <div className="grid grid-cols-1 gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
        <div className="flex min-w-0 flex-col gap-5">
          <RecentChecksTable items={items} isLoading={isLoading} error={error} />
        </div>
        <div className="flex flex-col gap-5">
          <Can I="contract.upload">
            <QuickStart />
          </Can>
          <BusinessSummary total={total ?? undefined} isLoading={isLoading} error={error} />
          <OrgCard
            user={meQuery.data}
            isLoading={meQuery.isLoading}
            error={meQuery.error ?? undefined}
          />
        </div>
      </div>

      <TrustFooter />
    </div>
  );
}
