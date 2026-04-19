// useDeleteContract — React-хук на базе useMutation (§7.5, §16.2, §17.3).
//
// Оптимистичное обновление (soft-delete → status='DELETED'):
//   1. `onMutate` — cancelQueries(qk.contracts.all), snapshot qk.contracts.byId
//      + queries матч ['contracts','list']. Применяем:
//        • byId: patch status='DELETED' + updated_at=now (ресурс сохраняется).
//        • list: фильтруем item из items[], decrement total (дефолтные листы
//          показывают только ACTIVE; архитектурное решение — §17.3).
//   2. `onError` — rollback обоих снапшотов.
//   3. `onSuccess` — финализирует статус из server-ответа.
//   4. `onSettled` — invalidate для финальной синхронизации.
//
// Подтверждение действия — через ConfirmDeleteContractModal (ui/), не здесь:
// хук остаётся чистой data-layer абстракцией.
import {
  type QueryKey,
  useMutation,
  type UseMutationResult,
  useQueryClient,
} from '@tanstack/react-query';
import { useCallback, useRef } from 'react';

import { type OrchestratorError, qk, toUserMessage, type UserMessage } from '@/shared/api';
import type { components } from '@/shared/api/openapi';

import { deleteContract } from '../api/delete-contract';
import type { DeleteContractInput, DeleteContractResponse } from './types';

type ContractDetails = components['schemas']['ContractDetails'];
type ContractList = components['schemas']['ContractList'];

export interface UseDeleteContractOptions {
  onSuccess?: (data: DeleteContractResponse) => void;
  onError?: (err: OrchestratorError, userMessage: UserMessage) => void;
}

export interface UseDeleteContractResult extends Omit<
  UseMutationResult<
    DeleteContractResponse,
    OrchestratorError,
    DeleteContractInput,
    DeleteContractContext
  >,
  'mutate' | 'mutateAsync'
> {
  remove: (input: DeleteContractInput) => void;
  removeAsync: (input: DeleteContractInput) => Promise<DeleteContractResponse>;
}

interface DeleteContractContext {
  previousById: ContractDetails | undefined;
  previousLists: Array<[QueryKey, ContractList | undefined]>;
}

export function useDeleteContract(opts: UseDeleteContractOptions = {}): UseDeleteContractResult {
  const queryClient = useQueryClient();
  const optsRef = useRef(opts);
  optsRef.current = opts;

  const mutation = useMutation<
    DeleteContractResponse,
    OrchestratorError,
    DeleteContractInput,
    DeleteContractContext
  >({
    mutationFn: (input) => deleteContract(input),

    onMutate: async (input) => {
      await queryClient.cancelQueries({ queryKey: qk.contracts.all });

      const previousById = queryClient.getQueryData<ContractDetails>(
        qk.contracts.byId(input.contractId),
      );
      const previousLists = queryClient.getQueriesData<ContractList>({
        queryKey: ['contracts', 'list'],
      });

      const nowIso = new Date().toISOString();
      if (previousById !== undefined) {
        queryClient.setQueryData<ContractDetails>(qk.contracts.byId(input.contractId), {
          ...previousById,
          status: 'DELETED',
          updated_at: nowIso,
        });
      }
      for (const [key] of previousLists) {
        queryClient.setQueryData<ContractList>(key, (current) => {
          if (!current?.items) return current;
          const filtered = current.items.filter((item) => item.contract_id !== input.contractId);
          const removed = filtered.length !== current.items.length;
          return {
            ...current,
            items: filtered,
            ...(removed &&
              typeof current.total === 'number' && {
                total: Math.max(0, current.total - 1),
              }),
          };
        });
      }

      return { previousById, previousLists };
    },

    onError: (err, input, context) => {
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
      const existingById = queryClient.getQueryData<ContractDetails>(
        qk.contracts.byId(input.contractId),
      );
      if (existingById !== undefined) {
        queryClient.setQueryData<ContractDetails>(qk.contracts.byId(input.contractId), {
          ...existingById,
          ...(data.status !== undefined && { status: data.status }),
          ...(data.updated_at !== undefined && { updated_at: data.updated_at }),
        });
      }
      optsRef.current.onSuccess?.(data);
    },

    onSettled: (_data, _err, input) => {
      void queryClient.invalidateQueries({ queryKey: qk.contracts.byId(input.contractId) });
      void queryClient.invalidateQueries({ queryKey: ['contracts', 'list'] });
    },
  });

  const remove = useCallback((input: DeleteContractInput) => mutation.mutate(input), [mutation]);
  const removeAsync = useCallback(
    (input: DeleteContractInput) => mutation.mutateAsync(input),
    [mutation],
  );

  const { mutate: _m, mutateAsync: _ma, ...rest } = mutation;
  void _m;
  void _ma;
  return { ...rest, remove, removeAsync };
}
