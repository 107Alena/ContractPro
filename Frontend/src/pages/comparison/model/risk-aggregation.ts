// Агрегация per-version рисков (GET /risks) в типы виджета version-compare
// для секций сравнения (FE-TASK-048). Виджет — чистое представление и
// получает агрегаты готовыми (см. widgets/version-compare/model/types), поэтому
// маппинг API-формы RiskList → RiskProfileSnapshot / ComparisonRisksGroups
// делается здесь, на уровне страницы.
//
// Источник: useRisks(base) + useRisks(target). Если у версии нет артефакта
// (не READY / нет прав / 404) → RiskList undefined → профиль undefined,
// группы пустые; виджеты показывают честные плейсхолдеры. Никаких выдуманных
// чисел.
import type { components } from '@/shared/api/openapi';
import type {
  ComparisonRiskItem,
  ComparisonRisksGroups,
  RiskProfileSnapshot,
} from '@/widgets/version-compare';

type RiskList = components['schemas']['RiskList'];
type Risk = components['schemas']['Risk'];

/** RiskList.risk_profile → RiskProfileSnapshot; undefined, если профиля нет. */
export function riskListToSnapshot(
  riskList: RiskList | undefined,
): RiskProfileSnapshot | undefined {
  const profile = riskList?.risk_profile;
  if (!profile) return undefined;
  return {
    high: profile.high_count ?? 0,
    medium: profile.medium_count ?? 0,
    low: profile.low_count ?? 0,
  };
}

// Ключ матчинга риска между версиями: стабильный id, иначе ссылка на пункт,
// иначе описание. Риски без любого из них в матчинг не попадают (key '').
function riskKey(risk: Risk): string {
  return risk.id ?? risk.clause_ref ?? risk.description ?? '';
}

function toItem(risk: Risk): ComparisonRiskItem {
  return {
    id: riskKey(risk) || 'risk',
    title: risk.description ?? risk.clause_ref ?? 'Риск',
    // level в схеме опционален; отсутствие трактуем как 'low' (наименее
    // тревожный дефолт — не завышаем риск).
    level: risk.level ?? 'low',
    ...(risk.clause_ref ? { category: risk.clause_ref } : {}),
  };
}

/**
 * Группирует риски двух версий по дельте через матчинг по riskKey:
 * resolved — были в base, нет в target; introduced — новые в target;
 * unchanged — присутствуют в обеих (берём версию из target).
 */
export function groupComparisonRisks(
  baseRiskList: RiskList | undefined,
  targetRiskList: RiskList | undefined,
): ComparisonRisksGroups {
  const base = baseRiskList?.risks ?? [];
  const target = targetRiskList?.risks ?? [];
  const baseKeys = new Set(base.map(riskKey));
  const targetKeys = new Set(target.map(riskKey));
  return {
    resolved: base.filter((r) => !targetKeys.has(riskKey(r))).map(toItem),
    introduced: target.filter((r) => !baseKeys.has(riskKey(r))).map(toItem),
    unchanged: target.filter((r) => baseKeys.has(riskKey(r))).map(toItem),
  };
}
