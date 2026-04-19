import { describe, expect, it } from 'vitest';

import { computeRiskDelta, computeVerdict } from './compute-verdict';

describe('computeVerdict', () => {
  it('оба undefined → unchanged', () => {
    expect(computeVerdict(undefined, undefined)).toBe('unchanged');
  });

  it('равные профили → unchanged', () => {
    expect(computeVerdict({ high: 2, medium: 3, low: 5 }, { high: 2, medium: 3, low: 5 })).toBe(
      'unchanged',
    );
  });

  it('target меньше high+medium → better', () => {
    expect(computeVerdict({ high: 4, medium: 2, low: 1 }, { high: 1, medium: 2, low: 1 })).toBe(
      'better',
    );
  });

  it('target больше high+medium → worse', () => {
    expect(computeVerdict({ high: 1, medium: 1, low: 0 }, { high: 3, medium: 2, low: 0 })).toBe(
      'worse',
    );
  });

  it('high упал, medium вырос → mixed (встречное движение)', () => {
    expect(computeVerdict({ high: 3, medium: 1, low: 2 }, { high: 1, medium: 4, low: 2 })).toBe(
      'mixed',
    );
  });

  it('high вырос, medium упал → mixed (встречное движение)', () => {
    expect(computeVerdict({ high: 1, medium: 3, low: 0 }, { high: 3, medium: 1, low: 0 })).toBe(
      'mixed',
    );
  });

  it('base undefined, target пустой → unchanged', () => {
    expect(computeVerdict(undefined, { high: 0, medium: 0, low: 0 })).toBe('unchanged');
  });
});

describe('computeRiskDelta', () => {
  it('возвращает {0,0,0} если оба undefined', () => {
    expect(computeRiskDelta()).toEqual({ high: 0, medium: 0, low: 0 });
  });

  it('считает target - base поэлементно', () => {
    expect(
      computeRiskDelta({ high: 5, medium: 3, low: 2 }, { high: 2, medium: 4, low: 0 }),
    ).toEqual({ high: -3, medium: 1, low: -2 });
  });

  it('обрабатывает только base (target=undefined) — дельта = -base', () => {
    expect(computeRiskDelta({ high: 1, medium: 2, low: 3 })).toEqual({
      high: -1,
      medium: -2,
      low: -3,
    });
  });
});
