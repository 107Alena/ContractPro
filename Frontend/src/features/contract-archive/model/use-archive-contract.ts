// useArchiveContract — React-хук на базе useMutation (§7.5, §16.2, §17.3).
//
// Оптимистичное обновление:
//   1. `onMutate` — cancelQueries(qk.contracts.all), snapshot of:
//        • qk.contracts.byId(cid) — ContractDetails;
//        • queries matching ['contracts','list'] — ContractList в разных params.
//      Затем setQueryData патчит status='ARCHIVED' + updated_at=now. Важно:
//      processing_status НЕ трогаем — это derived state из DP-пайплайна.
//   2. `onError` — rollback обоих снапшотов + вызов onError(err, userMessage).
//   3. `onSuccess` — setQueryData из server-ответа (свежий ContractSummary),
//      rollback'а не будет. onSuccess callback pages.
//   4. `onSettled` — invalidateQueries для финальной синхронизации.
//
// REQUEST_ABORTED фильтруется от UX (user-driven отмена). 409/500 → onError с
// toUserMessage.
import {
  type QueryKey,
  useMutation,
  type UseMutationResult,
  useQueryClient,
} from '@tanstack/react-query';
import { useCallback, useRef } from 'react';

import { type OrchestratorError, qk, toUserMessage, type UserMessage } from '@/shared/api';
import type { components } from '@/shared/api/openapi';

import { archiveContract } from '../api/archive-contract';
import type { ArchiveContractInput, ArchiveContractResponse, ContractSummary } from './types';

type ContractDetails = components['schemas']['ContractDetails'];
type ContractList = components['schemas']['ContractList'];

export interface UseArchiveContractOptions {
  onSuccess?: (data: ArchiveContractResponse) => void;
  onError?: (err: OrchestratorError, userMessage: UserMessage) => void;
}

export interface UseArchiveContractResult extends Omit<
  UseMutationResult<
    ArchiveContractResponse,
    OrchestratorError,
    ArchiveContractInput,
    ArchiveContractContext
  >,
  'mutate' | 'mutateAsync'
> {
  archive: (input: ArchiveContractInput) => void;
  archiveAsync: (input: ArchiveContractInput) => Promise<ArchiveContractResponse>;
}

interface ArchiveContractContext {
  /** Snapshot qk.contracts.byId(cid) до мутации. undefined = ключа не было. */
  previousById: ContractDetails | undefined;
  /** Snapshot'ы query-keys ['contracts','list', ...] с их значениями. */
  previousLists: Array<[QueryKey, ContractList | undefined]>;
}

export function useArchiveContract(opts: UseArchiveContractOptions = {}): UseArchiveContractResult {
  const queryClient = useQueryClient();
  const optsRef = useRef(opts);
  optsRef.current = opts;

  const mutation = useMutation<
    ArchiveContractResponse,
    OrchestratorError,
    ArchiveContractInput,
    ArchiveContractContext
  >({
    mutationFn: (input) => archiveContract(input),

    onMutate: async (input) => {
      // 1. Останавливаем in-flight рефетчи, иначе они могут перезаписать оптимистик.
      await queryClient.cancelQueries({ queryKey: qk.contracts.all });

      // 2. Снапшоты.
      const previousById = queryClient.getQueryData<ContractDetails>(
        qk.contracts.byId(input.contractId),
      );
      const previousLists = queryClient.getQueriesData<ContractList>({
        queryKey: ['contracts', 'list'],
      });

      // 3. Применяем оптимистик.
      const nowIso = new Date().toISOString();
      if (previousById !== undefined) {
        queryClient.setQueryData<ContractDetails>(qk.contracts.byId(input.contractId), {
          ...previousById,
          status: 'ARCHIVED',
          updated_at: nowIso,
        });
      }
      for (const [key] of previousLists) {
        queryClient.setQueryData<ContractList>(key, (current) => {
          if (!current) return current;
          if (!current.items) return current;
          const items: ContractSummary[] = current.items.map((item) =>
            item.contract_id === input.contractId
              ? ({
                  ...item,
                  status: 'ARCHIVED' as const,
                  updated_at: nowIso,
                } satisfies ContractSummary)
              : item,
          );
          return { ...current, items };
        });
      }

      return { previousById, previousLists };
    },

    onError: (err, input, context) => {
      // Rollback обоих снапшотов. Важно: не вызывать setQueryData, если ключ
      // не существовал до мутации (оставляем undefined / missing).
      if (context) {
        if (context.previousById !== undefined) {
          queryClient.setQueryData(qk.contracts.byId(input.contractId), context.previousById);
        }
        for (const [key, prev] of context.previousLists) {
          queryClient.setQueryData(key, prev);
        }
      }

      if (err.error_code === 'REQUEST_ABORTED') return;
      optsRef.current.onError?.(err, toUserMessage(err));
    },

    onSuccess: (data, input) => {
      // Server-ответ — свежий ContractSummary. Заменяем статус в кэшах, чтобы
      // убрать предположение (updated_at из сервера, не локальный).
      const existingById = queryClient.getQueryData<ContractDetails>(
        qk.contracts.byId(input.contractId),
      );
      if (existingById !== undefined) {
        queryClient.setQueryData<ContractDetails>(qk.contracts.byId(input.contractId), {
          ...existingById,
          ...(data.status !== undefined && { status: data.status }),
          ...(data.title !== undefined && { title: data.title }),
          ...(data.updated_at !== undefined && { updated_at: data.updated_at }),
        });
      }
      optsRef.current.onSuccess?.(data);
    },

    onSettled: (_data, _err, input) => {
      // Финальная синхронизация: подтягиваем актуальные данные с сервера,
      // если кто-то подписан на эти ключи.
      void queryClient.invalidateQueries({ queryKey: qk.contracts.byId(input.contractId) });
      void queryClient.invalidateQueries({ queryKey: ['contracts', 'list'] });
    },
  });

  const archive = useCallback((input: ArchiveContractInput) => mutation.mutate(input), [mutation]);
  const archiveAsync = useCallback(
    (input: ArchiveContractInput) => mutation.mutateAsync(input),
    [mutation],
  );

  const { mutate: _m, mutateAsync: _ma, ...rest } = mutation;
  void _m;
  void _ma;
  return { ...rest, archive, archiveAsync };
}
