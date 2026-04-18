// Barrel: публичный API feature comparison-start (§6.1, §16.2 high-architecture).
//
// Импортировать ТОЛЬКО этот путь (FSD-граница). Потребители — ResultPage
// (FE-TASK-046) и widgets/version-compare (FE-TASK-047).
export type { GetDiffOptions } from './api/get-diff';
export { getDiff, getDiffEndpoint } from './api/get-diff';
export type { StartComparisonOptions } from './api/start-comparison';
export { startComparison, startComparisonEndpoint } from './api/start-comparison';
export { isDiffNotReadyError } from './lib/is-diff-not-ready';
export type {
  GetDiffInput,
  StartComparisonInput,
  StartComparisonResponse,
  VersionDiffResult,
  VersionDiffStructuralChange,
  VersionDiffTextChange,
} from './model/types';
export { useDiff, type UseDiffOptions } from './model/use-diff';
export {
  useStartComparison,
  type UseStartComparisonOptions,
  type UseStartComparisonResult,
} from './model/use-start-comparison';
