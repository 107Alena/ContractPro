import { describe, expect, it } from 'vitest';

import { RISK_LEVEL_META, RISK_LEVELS, type RiskLevel, riskLevelMeta } from './risk-level';

describe('RISK_LEVEL_META', () => {
  it.each(['high', 'medium', 'low'] as const)('has an entry for "%s"', (level) => {
    expect(RISK_LEVEL_META).toHaveProperty(level);
    expect(RISK_LEVEL_META[level].label).toBeTruthy();
    expect(RISK_LEVEL_META[level].legend).toBeTruthy();
  });

  it('maps "high" → danger, "medium" → warning, "low" → success', () => {
    expect(RISK_LEVEL_META.high.tone).toBe('danger');
    expect(RISK_LEVEL_META.medium.tone).toBe('warning');
    expect(RISK_LEVEL_META.low.tone).toBe('success');
  });

  it('labels use canonical ТЗ terms (высокий/средний/низкий риск)', () => {
    expect(RISK_LEVEL_META.high.label).toBe('Высокий риск');
    expect(RISK_LEVEL_META.medium.label).toBe('Средний риск');
    expect(RISK_LEVEL_META.low.label).toBe('Низкий риск');
  });

  it('legend — осмысленная строка >= 20 символов (не placeholder)', () => {
    for (const level of RISK_LEVELS) {
      expect(RISK_LEVEL_META[level].legend.length).toBeGreaterThanOrEqual(20);
    }
  });
});

describe('RISK_LEVELS', () => {
  it('exposes levels in severity-descending order (high→low)', () => {
    expect(RISK_LEVELS).toEqual(['high', 'medium', 'low']);
  });
});

describe('riskLevelMeta()', () => {
  it('returns the same reference as RISK_LEVEL_META', () => {
    for (const level of RISK_LEVELS) {
      expect(riskLevelMeta(level satisfies RiskLevel)).toBe(RISK_LEVEL_META[level]);
    }
  });
});
