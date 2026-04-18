// Доменные типы feature comparison-start.
//
// Endpoints (§7.5 api-specification):
//   POST /contracts/{contract_id}/compare
//     body: CompareRequest { base_version_id, target_version_id }
//     202:  CompareResponse { job_id, status }
//   GET  /contracts/{contract_id}/versions/{base_version_id}/diff/{target_version_id}
//     200: VersionDiff { base_version_id, target_version_id, text_diff_count,
//                        structural_diff_count, text_diffs[], structural_diffs[] }
//     404: DIFF_NOT_FOUND — сравнение ещё не готово / не запускалось.
//
// OpenAPI поля в CompareResponse и VersionDiff опциональны (optional set),
// narrow-функции в api/* подтверждают обязательные поля на runtime.
import type { components } from '@/shared/api/openapi';

type CompareResponseRaw = components['schemas']['CompareResponse'];
type VersionDiffRaw = components['schemas']['VersionDiff'];

export interface StartComparisonInput {
  contractId: string;
  baseVersionId: string;
  targetVersionId: string;
}

export interface StartComparisonResponse {
  jobId: string;
  status: string;
}

export interface GetDiffInput {
  contractId: string;
  baseVersionId: string;
  targetVersionId: string;
}

export type VersionDiffTextChange = NonNullable<VersionDiffRaw['text_diffs']>[number];
export type VersionDiffStructuralChange = NonNullable<VersionDiffRaw['structural_diffs']>[number];

export interface VersionDiffResult {
  baseVersionId: string;
  targetVersionId: string;
  textDiffCount: number;
  structuralDiffCount: number;
  textDiffs: VersionDiffTextChange[];
  structuralDiffs: VersionDiffStructuralChange[];
}

/** @internal — type-level smoke-test совместимости с OpenAPI. */
export type __CompareResponseRaw = CompareResponseRaw;
/** @internal — type-level smoke-test совместимости с OpenAPI. */
export type __VersionDiffRaw = VersionDiffRaw;
