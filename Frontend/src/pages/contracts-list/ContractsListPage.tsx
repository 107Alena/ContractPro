// ContractsListPage (FE-TASK-044) — экран «Документы» (Figma 7, §17.1/§17.3/§17.4).
// URL: /contracts (auth). Композиция:
//
//   ┌─────────────────────────────────────────────────────┐
//   │ <ContractsMetricsStrip> 4 KPI-карточки по статусу    │
//   ├─────────────────────────────────────────────────────┤
//   │ <WhatMattersCards> 4 счётчика по processing_status   │
//   ├─────────────────────────────────────────────────────┤
//   │ <SearchInput>   <FilterChips>                        │
//   ├─────────────────────────────────────────────────────┤
//   │ <DocumentsTable> server-side pagination+виртуал.     │
//   │ <PaginationControls>                                 │
//   └─────────────────────────────────────────────────────┘
//
// RBAC Pattern B (§5.6.1, §17.3): для BUSINESS_USER колонка «Действия» не
// отображается (useCan('contract.archive')). Роль не блокирует доступ к
// странице целиком — «Limited Access Role Restriction» скрывает только
// action-элементы; просмотр списка доступен.
//
// Pattern B rollback на сервере: если роль/сессия изменилась — useCan
// пересчитывается реактивно из session-store, колонка исчезает без
// refetch'а.
import { useCallback, useState } from 'react';
import { Link } from 'react-router-dom';

import { type ContractSummary } from '@/entities/contract';
import { type ArchiveContractInput, useArchiveContract } from '@/features/contract-archive';
import {
  ConfirmDeleteContractModal,
  type DeleteContractInput,
  useDeleteContract,
} from '@/features/contract-delete';
import { FilterChips } from '@/features/filters';
import { PaginationControls } from '@/features/pagination';
import { SearchInput } from '@/features/search';
import { isOrchestratorError, toUserMessage } from '@/shared/api';
import { useCan } from '@/shared/auth';
import { Button, buttonVariants, toast } from '@/shared/ui';
import { ContractsMetricsStrip } from '@/widgets/contracts-metrics-strip';
import { WhatMattersCards } from '@/widgets/dashboard-what-matters';
import { DocumentsTable } from '@/widgets/documents-table';

import { CONTRACTS_FILTER_DEFINITIONS } from './model/filter-definitions';
import { useContractsListQuery } from './model/use-contracts-list-query';

function getContractTitle(contract?: ContractSummary | null): string {
  return contract?.title ?? 'Без названия';
}

export function ContractsListPage(): JSX.Element {
  const {
    search,
    filters,
    pagination,
    query,
    items,
    total,
    isLoading,
    isFetching,
    isError,
    error,
    hasActiveFilters,
  } = useContractsListQuery();

  const canArchive = useCan('contract.archive');

  // ------------------ Row-actions: archive / delete -----------------

  const archiveMutation = useArchiveContract({
    onSuccess: () => {
      toast.success('Договор перемещён в архив');
    },
    onError: (_err, userMessage) => {
      toast.error({
        title: userMessage.title,
        ...(userMessage.hint ? { description: userMessage.hint } : {}),
      });
    },
  });

  const [pendingDelete, setPendingDelete] = useState<ContractSummary | null>(null);

  const deleteMutation = useDeleteContract({
    onSuccess: () => {
      setPendingDelete(null);
      toast.success('Договор удалён');
    },
    onError: (_err, userMessage) => {
      setPendingDelete(null);
      toast.error({
        title: userMessage.title,
        ...(userMessage.hint ? { description: userMessage.hint } : {}),
      });
    },
  });

  const handleArchive = useCallback(
    (contract: ContractSummary) => {
      if (!contract.contract_id) return;
      const input: ArchiveContractInput = { contractId: contract.contract_id };
      archiveMutation.archive(input);
    },
    [archiveMutation],
  );

  const handleOpenDelete = useCallback((contract: ContractSummary) => {
    setPendingDelete(contract);
  }, []);

  const handleConfirmDelete = useCallback(() => {
    if (!pendingDelete?.contract_id) {
      setPendingDelete(null);
      return;
    }
    const input: DeleteContractInput = { contractId: pendingDelete.contract_id };
    deleteMutation.remove(input);
  }, [pendingDelete, deleteMutation]);

  const handleRetry = useCallback(() => {
    void query.refetch();
  }, [query]);

  const handleClearFilters = useCallback(() => {
    filters.clear();
    search.clear();
  }, [filters, search]);

  // ------------------ Render-actions ------------------

  const renderRowActions = canArchive
    ? ({ contract }: { contract: ContractSummary }): JSX.Element | null => {
        if (!contract.contract_id) return null;
        const archived = contract.status === 'ARCHIVED';
        const deleted = contract.status === 'DELETED';
        return (
          <div
            className="flex items-center justify-end gap-1"
            data-testid={`row-actions-${contract.contract_id}`}
          >
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => handleArchive(contract)}
              disabled={archived || deleted || archiveMutation.isPending}
              data-testid={`row-archive-${contract.contract_id}`}
            >
              {archived ? 'В архиве' : 'В архив'}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => handleOpenDelete(contract)}
              disabled={deleted || deleteMutation.isPending}
              data-testid={`row-delete-${contract.contract_id}`}
            >
              Удалить
            </Button>
          </div>
        );
      }
    : undefined;

  const errorMessage = ((): string => {
    if (!error) return '';
    if (isOrchestratorError(error)) {
      return toUserMessage(error).title;
    }
    return 'Произошла непредвиденная ошибка. Повторите попытку.';
  })();

  return (
    <main
      data-testid="page-contracts-list"
      className="mx-auto flex w-full max-w-7xl flex-col gap-6 px-4 py-6 md:px-6 md:py-8"
    >
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold text-fg">Документы</h1>
          <p className="mt-1 text-sm text-fg-muted">Все загруженные договоры вашей организации.</p>
        </div>
        <Link
          to="/contracts/new"
          className={buttonVariants({ variant: 'primary', size: 'md' })}
          data-testid="contracts-list-new"
        >
          Загрузить договор
        </Link>
      </header>

      <ContractsMetricsStrip
        items={items}
        total={total}
        isLoading={isLoading}
        error={isError ? error : undefined}
      />

      <WhatMattersCards
        items={items}
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
            ariaLabel="Поиск по списку договоров"
            className="w-full md:max-w-md"
            data-testid="contracts-list-search"
          />
        </div>
        <FilterChips
          definitions={CONTRACTS_FILTER_DEFINITIONS}
          values={filters.values}
          onToggleOption={filters.toggleOption}
          onClear={filters.clear}
        />
      </section>

      <DocumentsTable
        items={items}
        isLoading={isLoading}
        isFetching={isFetching}
        error={isError ? error : undefined}
        onRetry={handleRetry}
        hasActiveFilters={hasActiveFilters}
        filteredEmptyState={
          <div
            className="flex flex-col items-center gap-2 text-fg-muted"
            data-testid="documents-table-empty-filtered"
          >
            <p className="text-sm font-medium text-fg">По вашему запросу ничего не найдено</p>
            <p className="text-xs">Попробуйте изменить фильтры или поисковый запрос.</p>
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={handleClearFilters}
              data-testid="contracts-list-clear-filters"
            >
              Сбросить фильтры
            </Button>
          </div>
        }
        {...(renderRowActions ? { renderRowActions } : {})}
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

      {canArchive ? (
        <ConfirmDeleteContractModal
          open={pendingDelete != null}
          onOpenChange={(next) => {
            if (next) return;
            // Блокируем закрытие модалки, пока выполняется мутация, — иначе
            // пользователь потеряет сигнал о том, что запрос ещё в работе.
            if (deleteMutation.isPending) return;
            setPendingDelete(null);
          }}
          contractTitle={getContractTitle(pendingDelete)}
          isPending={deleteMutation.isPending}
          onConfirm={handleConfirmDelete}
        />
      ) : null}

      {isError ? (
        <p role="alert" className="text-sm text-danger" data-testid="contracts-list-error">
          {errorMessage}
        </p>
      ) : null}
    </main>
  );
}
