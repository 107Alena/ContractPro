import { describe, expect, it } from 'vitest';

import type { components } from '@/shared/api/openapi';

import { groupComparisonRisks, riskListToSnapshot } from './risk-aggregation';

type RiskList = components['schemas']['RiskList'];

describe('riskListToSnapshot', () => {
  it('undefined / без risk_profile → undefined', () => {
    expect(riskListToSnapshot(undefined)).toBeUndefined();
    expect(riskListToSnapshot({ risks: [] })).toBeUndefined();
  });

  it('маппит risk_profile в snapshot (отсутствующие счётчики → 0)', () => {
    const rl: RiskList = {
      risk_profile: { overall_level: 'high', high_count: 2, medium_count: 3 },
    };
    expect(riskListToSnapshot(rl)).toEqual({ high: 2, medium: 3, low: 0 });
  });
});

describe('groupComparisonRisks', () => {
  const base: RiskList = {
    risks: [
      { id: 'r1', level: 'high', description: 'Односторонняя неустойка' },
      { id: 'r2', level: 'medium', description: 'Нет срока оплаты' },
      { id: 'r3', level: 'low', description: 'Опечатка в реквизитах' },
    ],
  };
  const target: RiskList = {
    risks: [
      { id: 'r2', level: 'medium', description: 'Нет срока оплаты' }, // unchanged
      { id: 'r3', level: 'low', description: 'Опечатка в реквизитах' }, // unchanged
      { id: 'r4', level: 'medium', description: 'Неопределённый промежуточный платёж' }, // introduced
    ],
  };

  it('делит риски на resolved / introduced / unchanged по id', () => {
    const g = groupComparisonRisks(base, target);
    expect(g.resolved.map((r) => r.id)).toEqual(['r1']);
    expect(g.introduced.map((r) => r.id)).toEqual(['r4']);
    expect(g.unchanged.map((r) => r.id)).toEqual(['r2', 'r3']);
  });

  it('пустые / undefined списки → пустые группы', () => {
    expect(groupComparisonRisks(undefined, undefined)).toEqual({
      resolved: [],
      introduced: [],
      unchanged: [],
    });
  });

  it('все риски новые, если base пуст', () => {
    const g = groupComparisonRisks({ risks: [] }, target);
    expect(g.introduced).toHaveLength(3);
    expect(g.resolved).toHaveLength(0);
    expect(g.unchanged).toHaveLength(0);
  });

  it('матчинг по clause_ref, если нет id; level по умолчанию low', () => {
    const b: RiskList = { risks: [{ clause_ref: 'п.7.2' }] };
    const t: RiskList = { risks: [{ clause_ref: 'п.7.2' }] };
    const g = groupComparisonRisks(b, t);
    expect(g.unchanged).toHaveLength(1);
    expect(g.unchanged[0]?.level).toBe('low');
    expect(g.unchanged[0]?.category).toBe('п.7.2');
  });

  it('НЕидентифицируемые риски (нет id/clause_ref/description) не слипаются в unchanged', () => {
    // Два разных безымянных риска НЕ должны ложно совпасть по пустому ключу.
    const b: RiskList = { risks: [{ level: 'high' }] };
    const t: RiskList = { risks: [{ level: 'medium' }] };
    const g = groupComparisonRisks(b, t);
    expect(g.unchanged).toHaveLength(0); // ключевой инвариант: НЕ unchanged
    expect(g.resolved).toHaveLength(1); // безымянный из base → resolved
    expect(g.introduced).toHaveLength(1); // безымянный из target → introduced
    // уникальные fallback-id (нет коллизии React-ключей)
    expect(g.resolved[0]?.id).not.toBe(g.introduced[0]?.id);
  });
});
