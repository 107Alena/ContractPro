// ContractsListPage (FE-TASK-044) — экран «Документы» (Figma 7, §17.1/§17.3/§17.4).
// URL: /contracts (auth). Композиция (Figma 193:2, этап 4.5):
//
//   ┌─────────────────────────────────────────────────────┐
//   │ PageIntro (только заголовок + описание; header-CTA    │
//   │   «Загрузить PDF» / «Новая проверка» убраны)          │
//   │ <ContractsMetricsStrip> 5-stat сводка                │
//   │ <CurrentActions> «Что важно сейчас» (= dashboard)    │
//   │ Card[ <SearchInput>  <FilterChips> ]                 │
//   │ <DocumentsTable> server-side pagination + виртуал.   │
//   │ <PaginationControls>  ·  <TrustFooter>               │
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
import {
  Button,
  buttonVariants,
  Card,
  Popover,
  PopoverContent,
  PopoverTrigger,
  toast,
} from '@/shared/ui';
import { ContractsMetricsStrip } from '@/widgets/contracts-metrics-strip';
import { CurrentActions } from '@/widgets/dashboard-current-actions';
import { TrustFooter } from '@/widgets/dashboard-trust-footer';
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

  // Действия в строке (Figma 202:24): Результат / Сравнить доступны всем
  // (read-навигация); архивация/удаление — в ⋯-меню только при RBAC-доступе
  // (canArchive). Колонка показывается всем ролям.
  const renderRowActions = ({ contract }: { contract: ContractSummary }): JSX.Element | null => {
    const id = contract.contract_id;
    if (!id) return null;
    const archived = contract.status === 'ARCHIVED';
    const deleted = contract.status === 'DELETED';
    const encoded = encodeURIComponent(id);
    // Stage 5: «Сравнить» только при ≥2 версий — иначе /compare откроется на
    // «Версии не выбраны» (пресет невозможен — version-UUID нет в ContractSummary).
    const canCompareRow = (contract.current_version_number ?? 0) >= 2;
    return (
      <div className="flex items-center justify-end gap-1" data-testid={`row-actions-${id}`}>
        <Link
          to={`/contracts/${encoded}`}
          className={buttonVariants({ variant: 'ghost', size: 'sm' })}
        >
          Результат
        </Link>
        {canCompareRow ? (
          <Link
            to={`/contracts/${encoded}/compare`}
            className={buttonVariants({ variant: 'ghost', size: 'sm' })}
            data-testid={`row-compare-${id}`}
          >
            Сравнить
          </Link>
        ) : null}
        {canArchive ? (
          <Popover>
            <PopoverTrigger asChild>
              <button
                type="button"
                aria-label="Ещё действия"
                data-testid={`row-more-${id}`}
                className="inline-flex size-8 items-center justify-center rounded-md text-fg-muted hover:bg-bg-muted focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2"
              >
                ⋯
              </button>
            </PopoverTrigger>
            <PopoverContent size="sm" align="end" className="flex flex-col gap-1">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                fullWidth
                onClick={() => handleArchive(contract)}
                disabled={archived || deleted || archiveMutation.isPending}
                data-testid={`row-archive-${id}`}
                className="justify-start"
              >
                {archived ? 'В архиве' : 'В архив'}
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                fullWidth
                onClick={() => handleOpenDelete(contract)}
                disabled={deleted || deleteMutation.isPending}
                data-testid={`row-delete-${id}`}
                className="justify-start"
              >
                Удалить
              </Button>
            </PopoverContent>
          </Popover>
        ) : null}
      </div>
    );
  };

  const errorMessage = ((): string => {
    if (!error) return '';
    if (isOrchestratorError(error)) {
      return toUserMessage(error).title;
    }
    return 'Произошла непредвиденная ошибка. Повторите попытку.';
  })();

  return (
    <div
      data-testid="page-contracts-list"
      className="mx-auto flex w-full max-w-7xl flex-col gap-6 px-4 py-6 md:px-8 md:py-8"
    >
      <header className="flex flex-col gap-1.5">
        <h1 className="text-24 font-bold text-fg">Документы и история проверок</h1>
        <p className="text-15 text-fg-muted">
          Находите договоры, отслеживайте статус проверок и открывайте результаты анализа.
        </p>
      </header>

      <ContractsMetricsStrip
        items={items}
        total={total}
        isLoading={isLoading}
        error={isError ? error : undefined}
      />

      <CurrentActions items={items} isLoading={isLoading} error={isError ? error : undefined} />

      <Card aria-label="Поиск и фильтры" className="flex flex-col gap-3 p-4">
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
      </Card>

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
        renderRowActions={renderRowActions}
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

      <TrustFooter />
    </div>
  );
}
