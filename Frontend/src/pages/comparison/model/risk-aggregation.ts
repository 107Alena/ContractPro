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
// иначе описание. null — риск НЕидентифицируем (нет ни одного из полей): такой
// риск не матчится с другими (иначе два разных безымянных риска ложно слиплись
// бы в «unchanged»), а попадает в resolved (если в base) / introduced (в target).
function riskKey(risk: Risk): string | null {
  return risk.id ?? risk.clause_ref ?? risk.description ?? null;
}

// Совпал ли риск с риском другой версии: только по непустому ключу.
function matches(risk: Risk, otherKeys: ReadonlySet<string>): boolean {
  const key = riskKey(risk);
  return key !== null && otherKeys.has(key);
}

function toItem(risk: Risk, fallbackId: string): ComparisonRiskItem {
  return {
    id: riskKey(risk) ?? fallbackId,
    title: risk.description ?? risk.clause_ref ?? 'Риск',
    // level в схеме опционален; отсутствие трактуем как 'low' (наименее
    // тревожный дефолт — не завышаем риск).
    level: risk.level ?? 'low',
    ...(risk.clause_ref ? { category: risk.clause_ref } : {}),
  };
}

// Множество непустых ключей версии (null-ключи исключаются из матчинга).
function keySet(risks: readonly Risk[]): Set<string> {
  return new Set(risks.map(riskKey).filter((k): k is string => k !== null));
}

/**
 * Группирует риски двух версий по дельте через матчинг по riskKey:
 * resolved — были в base, нет в target; introduced — новые в target;
 * unchanged — присутствуют в обеих (берём версию из target).
 * НЕидентифицируемые риски (key=null) никогда не считаются unchanged.
 */
export function groupComparisonRisks(
  baseRiskList: RiskList | undefined,
  targetRiskList: RiskList | undefined,
): ComparisonRisksGroups {
  const base = baseRiskList?.risks ?? [];
  const target = targetRiskList?.risks ?? [];
  const baseKeys = keySet(base);
  const targetKeys = keySet(target);
  return {
    resolved: base.filter((r) => !matches(r, targetKeys)).map((r, i) => toItem(r, `resolved-${i}`)),
    introduced: target
      .filter((r) => !matches(r, baseKeys))
      .map((r, i) => toItem(r, `introduced-${i}`)),
    unchanged: target
      .filter((r) => matches(r, baseKeys))
      .map((r, i) => toItem(r, `unchanged-${i}`)),
  };
}
