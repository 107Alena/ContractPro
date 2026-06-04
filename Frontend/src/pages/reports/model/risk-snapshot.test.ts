import { describe, expect, it } from 'vitest';

import { toReportRiskProfile } from './risk-snapshot';

describe('toReportRiskProfile', () => {
  it('нет risk_profile → null', () => {
    expect(toReportRiskProfile(undefined)).toBeNull();
    expect(toReportRiskProfile({ risks: [] })).toBeNull();
  });

  it('берёт overall_level из бэка + счётчики', () => {
    expect(
      toReportRiskProfile({
        risks: [],
        risk_profile: { overall_level: 'medium', high_count: 2, medium_count: 3, low_count: 1 },
      }),
    ).toEqual({ level: 'medium', high: 2, medium: 3, low: 1 });
  });

  it('нет overall_level → level=null + реальные счётчики (вердикт НЕ синтезируем)', () => {
    expect(
      toReportRiskProfile({
        risks: [],
        risk_profile: { high_count: 1, medium_count: 5, low_count: 9 },
      }),
    ).toEqual({ level: null, high: 1, medium: 5, low: 9 });
  });

  it('нет вердикта и все счётчики 0 → null (нечего показывать)', () => {
    expect(
      toReportRiskProfile({
        risks: [],
        risk_profile: { high_count: 0, medium_count: 0, low_count: 0 },
      }),
    ).toBeNull();
  });
});
