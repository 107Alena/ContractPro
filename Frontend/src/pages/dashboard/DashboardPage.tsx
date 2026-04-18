// DashboardPage (FE-TASK-042) — главный экран после логина.
// Архитектура: §17.1 (auth, GET /users/me + GET /contracts?size=5 + SSE),
// §17.4 (widgets: WhatMattersCards, LastCheckCard, QuickStart, OrgCard,
// RecentChecksTable, KeyRisksCards), §9.3 (error-state), §5.6 (RBAC guards).
//
// 4 состояния (из AC FE-TASK-042):
//   Default — данные загружены, все виджеты отрисованы.
//   Loading — isLoading=true на useMe/useContracts → виджеты показывают spinner.
//   Empty — useMe ок, /contracts пустой → LastCheckCard/RecentChecks/KeyRisks
//           в Empty-варианте; WhatMatters показывает нули; QuickStart видим.
//   Error — useContracts.error → соответствующие виджеты в error-варианте;
//           useMe.error → OrgCard в error-варианте.
//
// SSE: useEventStream без document_id подписывается на глобальный feed (§7.7);
// status-update попадает в qk.contracts.status(id,vid); /contracts сам по себе
// не инвалидируется (snapshot-цельность на момент запроса), но refetch по
// staleTime актуализирует снимок.
import { useContracts } from '@/entities/contract';
import { useMe } from '@/entities/user';
import { useEventStream } from '@/shared/api';
import { Can } from '@/shared/auth';
import { KeyRisksCards } from '@/widgets/dashboard-key-risks';
import { LastCheckCard } from '@/widgets/dashboard-last-check';
import { OrgCard } from '@/widgets/dashboard-org-card';
import { QuickStart } from '@/widgets/dashboard-quick-start';
import { RecentChecksTable } from '@/widgets/dashboard-recent-checks';
import { WhatMattersCards } from '@/widgets/dashboard-what-matters';

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

  return (
    <main
      data-testid="page-dashboard"
      className="mx-auto flex w-full max-w-7xl flex-col gap-6 px-4 py-6 md:px-6 md:py-8"
    >
      <header>
        <h1 className="text-2xl font-semibold text-fg">Главная</h1>
        <p className="mt-1 text-sm text-fg-muted">
          Быстрый обзор проверок договоров и состояния вашей организации.
        </p>
      </header>

      <WhatMattersCards
        items={items}
        total={total ?? undefined}
        isLoading={contractsQuery.isLoading}
        error={contractsQuery.error ?? undefined}
      />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <div className="md:col-span-2">
          <LastCheckCard
            contract={latestContract}
            isLoading={contractsQuery.isLoading}
            error={contractsQuery.error ?? undefined}
          />
        </div>
        <div>
          <OrgCard
            user={meQuery.data}
            isLoading={meQuery.isLoading}
            error={meQuery.error ?? undefined}
          />
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <Can I="contract.upload">
          <QuickStart />
        </Can>
        <Can I="risks.view">
          <div className="md:col-span-2">
            <KeyRisksCards
              items={items}
              isLoading={contractsQuery.isLoading}
              error={contractsQuery.error ?? undefined}
            />
          </div>
        </Can>
      </div>

      <RecentChecksTable
        items={items}
        isLoading={contractsQuery.isLoading}
        error={contractsQuery.error ?? undefined}
      />
    </main>
  );
}
