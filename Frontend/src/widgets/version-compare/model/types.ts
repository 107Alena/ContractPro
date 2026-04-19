// Доменные типы виджета version-compare (§17.4 high-architecture).
//
// Виджет — слой представления Compare-страницы:
//   ComparisonPage = Header + Verdict + Counters + Tabs + Table + Risk-delta +
//                    Sections + Risks-groups (см. §17.4).
// Все агрегаты считаются ВНЕ виджета (page/feature слой) и приходят сюда
// готовыми; виджет не делает API-вызовов и не знает про TanStack Query.

/** Метаданные одной версии договора (для VersionMetaHeader). */
export interface VersionMetadata {
  versionId: string;
  versionNumber?: number;
  title?: string;
  authorName?: string;
  /** ISO-строка (created_at из DM /versions). */
  createdAt?: string;
}

/** Итоговая оценка изменения профиля рисков в target по сравнению с base. */
export type ComparisonVerdict = 'better' | 'worse' | 'unchanged' | 'mixed';

/** Снимок профиля рисков на одной версии (high/medium/low — взвешенно). */
export interface RiskProfileSnapshot {
  high: number;
  medium: number;
  low: number;
}

/** Дельта (target - base) по уровням риска. Может быть отрицательной. */
export interface RiskProfileDeltaValue {
  high: number;
  medium: number;
  low: number;
}

/** Счётчики diff'а, агрегированные из VersionDiffResult.text/structural_diffs. */
export interface ChangeCountersValue {
  total: number;
  added: number;
  removed: number;
  modified: number;
  moved: number;
  textual: number;
  structural: number;
}

/** Активный фильтр TabsFilters → ChangesTable. */
export type ChangesFilter = 'all' | 'textual' | 'structural' | 'high-risk';

/** Сводка изменений по одному разделу/секции договора. */
export interface SectionDiffSummary {
  section: string;
  added: number;
  removed: number;
  modified: number;
}

/** Один риск из ComparisonRisksGroups. */
export interface ComparisonRiskItem {
  id: string;
  title: string;
  level: 'high' | 'medium' | 'low';
  category?: string;
}

/** Группировка рисков «исчезнувшие / новые / без изменений» между версиями. */
export interface ComparisonRisksGroups {
  /** Были в base, нет в target. */
  resolved: readonly ComparisonRiskItem[];
  /** Новые в target. */
  introduced: readonly ComparisonRiskItem[];
  /** Присутствуют в обеих версиях. */
  unchanged: readonly ComparisonRiskItem[];
}
