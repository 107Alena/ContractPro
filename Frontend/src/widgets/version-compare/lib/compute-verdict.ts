// Сравнение профилей рисков двух версий → ComparisonVerdict + дельта.
//
// Эвристика вердикта:
//   - 'unchanged' — суммарно high+medium совпадает, и по уровням нет
//     встречного движения (ни одно не выросло против другого).
//   - 'better'    — суммарно target.high+medium < base.high+medium и
//     ни один из high/medium не вырос.
//   - 'worse'     — суммарно target.high+medium > base.high+medium и
//     ни один из high/medium не уменьшился.
//   - 'mixed'     — встречное движение (например, high упал, medium вырос)
//     или суммы равны, но раскладка изменилась.
//
// Если оба профиля undefined — 'unchanged'. Если один undefined, считаем
// его как пустой (high=0, medium=0, low=0) — это даёт корректный сценарий
// «у base ничего не было, в target появились риски → worse».
import type { ComparisonVerdict, RiskProfileDeltaValue, RiskProfileSnapshot } from '../model/types';

const EMPTY: RiskProfileSnapshot = { high: 0, medium: 0, low: 0 };

function severitySum(profile: RiskProfileSnapshot): number {
  return profile.high + profile.medium;
}

export function computeVerdict(
  baseProfile: RiskProfileSnapshot | undefined,
  targetProfile: RiskProfileSnapshot | undefined,
): ComparisonVerdict {
  if (baseProfile === undefined && targetProfile === undefined) return 'unchanged';

  const base = baseProfile ?? EMPTY;
  const target = targetProfile ?? EMPTY;

  const dHigh = target.high - base.high;
  const dMed = target.medium - base.medium;
  const dSum = severitySum(target) - severitySum(base);

  // Встречное движение по уровням → mixed (даже если сумма не изменилась).
  const opposingHighMed = (dHigh > 0 && dMed < 0) || (dHigh < 0 && dMed > 0);
  if (opposingHighMed) return 'mixed';

  if (dSum < 0) return 'better';
  if (dSum > 0) return 'worse';
  return 'unchanged';
}

export function computeRiskDelta(
  baseProfile?: RiskProfileSnapshot,
  targetProfile?: RiskProfileSnapshot,
): RiskProfileDeltaValue {
  const base = baseProfile ?? EMPTY;
  const target = targetProfile ?? EMPTY;
  return {
    high: target.high - base.high,
    medium: target.medium - base.medium,
    low: target.low - base.low,
  };
}
