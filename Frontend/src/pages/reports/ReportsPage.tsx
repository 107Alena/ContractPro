// ReportsPage (FE-TASK-048) — экран «Отчёты» (Figma 9, §17.4, §5.6.1 Pattern B).
// URL: /reports (auth-guarded в router.tsx). Композиция:
//
//   ┌──────────────────────────────────────────────────────────┐
//   │ <ExpiredLinkBanner> (если ?share=expired)                 │
//   │ <ReportsMetrics>    4 KPI-карточки                        │
//   │ <SearchInput> + <FilterChips>                             │
//   │ ┌──────────────────────┬────────────────────────────────┐ │
//   │ │ <ReportsTable>       │ <ReportDetailPanel> (если      │ │
//   │ │ + <PaginationControls>│ выбрана строка)                │ │
//   │ └──────────────────────┴────────────────────────────────┘ │
//   │ <ExportShareModal> (если открыт из detail-panel)          │
//   └──────────────────────────────────────────────────────────┘
//
// RBAC: §5.6.1 Pattern B. Маршрут auth-guarded, экспорт/share — гейтятся
// useCanExport() внутри ReportDetailPanel и ExportShareModal. Full-route block
// (Pattern A) отложен до подтверждения дизайн-команды (см. §5.6.1 табл. 2
// строка для «9. Отчёты» frame 235:12).
//
// Archived/deleted договоры в реестре отчётов не показываются: use-reports-
// list-query.ts жёстко ставит status=ACTIVE.
import { useCallback, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

import { type ContractSummary } from '@/entities/contract';
import { FilterChips } from '@/features/filters';
import { PaginationControls } from '@/features/pagination';
import { SearchInput } from '@/features/search';
import { isOrchestratorError, toUserMessage } from '@/shared/api';
import { Button } from '@/shared/ui';
import { ExpiredLinkBanner } from '@/widgets/expired-link-banner';
import { ExportShareModal } from '@/widgets/export-share-modal';
import { ReportDetailPanel } from '@/widgets/report-detail-panel';
import { ReportsMetrics } from '@/widgets/reports-metrics';
import { ReportsTable } from '@/widgets/reports-table';

import { REPORTS_FILTER_DEFINITIONS } from './model/filter-definitions';
import { useReportsListQuery } from './model/use-reports-list-query';

interface ShareState {
  contractId: string;
  versionId: string;
}

const SHARE_PARAM_KEY = 'share';
const SHARE_EXPIRED_VALUE = 'expired';

export function ReportsPage(): JSX.Element {
  const {
    search,
    filters,
    pagination,
    query,
    items,
    rawItems,
    total,
    isLoading,
    isFetching,
    isError,
    error,
    hasActiveFilters,
  } = useReportsListQuery();

  const [searchParams, setSearchParams] = useSearchParams();
  const shareExpired = searchParams.get(SHARE_PARAM_KEY) === SHARE_EXPIRED_VALUE;

  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [shareState, setShareState] = useState<ShareState | null>(null);

  const selectedContract: ContractSummary | null =
    selectedId != null ? (items.find((c) => c.contract_id === selectedId) ?? null) : null;

  const handleRetry = useCallback(() => {
    void query.refetch();
  }, [query]);

  const handleSelectRow = useCallback((contract: ContractSummary): void => {
    if (!contract.contract_id) return;
    setSelectedId(contract.contract_id);
  }, []);

  const handleCloseDetail = useCallback((): void => {
    setSelectedId(null);
  }, []);

  const handleOpenShare = useCallback((input: ShareState): void => {
    setShareState(input);
  }, []);

  const handleShareOpenChange = useCallback((open: boolean): void => {
    if (!open) setShareState(null);
  }, []);

  const handleDismissExpired = useCallback((): void => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete(SHARE_PARAM_KEY);
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  const handleClearFilters = useCallback(() => {
    filters.clear();
    search.clear();
  }, [filters, search]);

  const errorMessage = ((): string => {
    if (!error) return '';
    if (isOrchestratorError(error)) {
      return toUserMessage(error).title;
    }
    return 'Произошла непредвиденная ошибка. Повторите попытку.';
  })();

  return (
    <main
      data-testid="page-reports"
      className="mx-auto flex w-full max-w-7xl flex-col gap-6 px-4 py-6 md:px-6 md:py-8"
    >
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-fg">Отчёты</h1>
          <p className="mt-1 text-sm text-fg-muted">
            Готовые проверки с возможностью экспорта и отправки ссылки.
          </p>
        </div>
      </header>

      <ExpiredLinkBanner visible={shareExpired} onDismiss={handleDismissExpired} />

      <ReportsMetrics
        items={rawItems}
        total={total}
        isLoading={isLoading}
        error={isError ? error : undefined}
      />

      <section
        aria-label="Поиск и фильтры"
        className="flex flex-col gap-3 rounded-md border border-border bg-bg p-4 shadow-sm"
      >
        <div className="flex flex-col gap-3 md:flex-row md:items-center">
          <SearchInput
            value={search.inputValue}
            onValueChange={search.setInputValue}
            isPending={search.isPending}
            placeholder="Поиск по названию договора…"
            ariaLabel="Поиск по реестру отчётов"
            className="w-full md:max-w-md"
            data-testid="reports-search"
          />
        </div>
        <FilterChips
          definitions={REPORTS_FILTER_DEFINITIONS}
          values={filters.values}
          onToggleOption={filters.toggleOption}
          onClear={filters.clear}
        />
      </section>

      <div className="flex flex-col gap-4 lg:flex-row lg:items-start">
        <div className="flex min-w-0 flex-1 flex-col gap-4">
          <ReportsTable
            items={items}
            isLoading={isLoading}
            isFetching={isFetching}
            error={isError ? error : undefined}
            onRetry={handleRetry}
            hasActiveFilters={hasActiveFilters}
            selectedId={selectedId}
            onSelectRow={handleSelectRow}
            filteredEmptyState={
              <div
                className="flex flex-col items-center gap-2 text-fg-muted"
                data-testid="reports-table-empty-filtered"
              >
                <p className="text-sm font-medium text-fg">По вашему запросу отчёты не найдены</p>
                <p className="text-xs">Попробуйте изменить фильтры или поисковый запрос.</p>
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  onClick={handleClearFilters}
                  data-testid="reports-clear-filters"
                >
                  Сбросить фильтры
                </Button>
              </div>
            }
          />

          {!isError && total > 0 ? (
            <PaginationControls
              page={pagination.page}
              size={pagination.size}
              total={total}
              onPageChange={pagination.setPage}
              onSizeChange={pagination.setSize}
              isLoading={isLoading}
              isFetching={isFetching}
            />
          ) : null}
        </div>

        {selectedContract ? (
          <ReportDetailPanel
            contract={selectedContract}
            onClose={handleCloseDetail}
            onOpenShare={handleOpenShare}
          />
        ) : null}
      </div>

      {shareState ? (
        <ExportShareModal
          open
          onOpenChange={handleShareOpenChange}
          contractId={shareState.contractId}
          versionId={shareState.versionId}
        />
      ) : null}

      {isError ? (
        <p role="alert" className="text-sm text-danger" data-testid="reports-error">
          {errorMessage}
        </p>
      ) : null}
    </main>
  );
}
