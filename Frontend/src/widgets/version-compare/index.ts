// Public API виджета version-compare (FSD: импортируется только через barrel).
// 8 компонентов представления + lib/computeChangeCounters/computeVerdict/
// computeRiskDelta/groupBySection. Все вычисления — pure, без сайд-эффектов.
export { computeChangeCounters } from './lib/compute-counters';
export { computeRiskDelta, computeVerdict } from './lib/compute-verdict';
export { groupBySection } from './lib/group-by-section';
export type {
  ChangeCountersValue,
  ChangesFilter,
  ComparisonRiskItem,
  ComparisonRisksGroups,
  ComparisonVerdict,
  RiskProfileDeltaValue,
  RiskProfileSnapshot,
  SectionDiffSummary,
  VersionMetadata,
} from './model/types';
export { ChangeCounters, type ChangeCountersProps } from './ui/change-counters';
export { ChangesTable, type ChangesTableProps } from './ui/changes-table';
export {
  ComparisonVerdictCard,
  type ComparisonVerdictCardProps,
} from './ui/comparison-verdict-card';
export { KeyDiffsBySection, type KeyDiffsBySectionProps } from './ui/key-diffs-by-section';
export { RiskProfileDelta, type RiskProfileDeltaProps } from './ui/risk-profile-delta';
export { RisksGroups, type RisksGroupsProps } from './ui/risks-groups';
export { TabsFilters, type TabsFiltersProps } from './ui/tabs-filters';
export { VersionMetaHeader, type VersionMetaHeaderProps } from './ui/version-meta-header';
