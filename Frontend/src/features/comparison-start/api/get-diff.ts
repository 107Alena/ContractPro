// GET /contracts/{contract_id}/versions/{base_version_id}/diff/{target_version_id}
// (§7.5 api-specification).
//
// Контракт: 200 VersionDiff с подсчётами и массивами изменений. 404
// DIFF_NOT_FOUND — сравнение ещё не готово (UX: "Сравнение ещё не готово",
// обрабатывается в useDiff через `isDiffNotReadyError`).
//
// Тонкая обёртка: вызывает axios, сужает тип ответа. Массивы могут быть
// отсутствующими в ответе — narrow подставляет [] (детерминированный API
// для consumer'ов, см. sequence-diagrams §8.7 "пустой diff").
import type { components } from '@/shared/api/openapi';

import type {
  GetDiffInput,
  VersionDiffResult,
  VersionDiffStructuralChange,
  VersionDiffTextChange,
} from '../model/types';
import { getHttpInstance } from './http';

function endpointFor(contractId: string, baseVersionId: string, targetVersionId: string): string {
  return (
    `/contracts/${encodeURIComponent(contractId)}` +
    `/versions/${encodeURIComponent(baseVersionId)}` +
    `/diff/${encodeURIComponent(targetVersionId)}`
  );
}

export interface GetDiffOptions {
  signal?: AbortSignal;
}

type RawResponse = components['schemas']['VersionDiff'];

function narrowResponse(raw: RawResponse, fallback: GetDiffInput): VersionDiffResult {
  const {
    base_version_id,
    target_version_id,
    text_diff_count,
    structural_diff_count,
    text_diffs,
    structural_diffs,
  } = raw;

  return {
    baseVersionId: typeof base_version_id === 'string' ? base_version_id : fallback.baseVersionId,
    targetVersionId:
      typeof target_version_id === 'string' ? target_version_id : fallback.targetVersionId,
    textDiffCount: typeof text_diff_count === 'number' ? text_diff_count : 0,
    structuralDiffCount:
      typeof structural_diff_count === 'number' ? structural_diff_count : 0,
    textDiffs: Array.isArray(text_diffs) ? (text_diffs as VersionDiffTextChange[]) : [],
    structuralDiffs: Array.isArray(structural_diffs)
      ? (structural_diffs as VersionDiffStructuralChange[])
      : [],
  };
}

/**
 * Получает результат сравнения версий. 404 DIFF_NOT_FOUND бросается axios-клиентом
 * как OrchestratorError — вызывающая сторона различает через `isDiffNotReadyError`.
 */
export async function getDiff(
  input: GetDiffInput,
  opts: GetDiffOptions = {},
): Promise<VersionDiffResult> {
  const http = getHttpInstance();
  const { data } = await http.get<RawResponse>(
    endpointFor(input.contractId, input.baseVersionId, input.targetVersionId),
    {
      ...(opts.signal && { signal: opts.signal }),
    },
  );
  return narrowResponse(data, input);
}

export { endpointFor as getDiffEndpoint };
