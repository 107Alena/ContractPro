// toReportRiskProfile — сворачивает RiskList (GET …/risks) в компактный
// view-model для риск-строки ReportDetailPanel (Figma 230:6).
//
// data-honesty: нет risk_profile (версия не READY / артефакта нет / 404) → null,
// панель покажет честный плейсхолдер, ничего не выдумываем (legal-данные).
// overall_level берём из бэка, иначе деривируем доминирующий уровень по счётчикам.
import { type RiskList } from '@/entities/result';
import { type RiskLevel } from '@/entities/risk';
import { type ReportRiskProfileView } from '@/widgets/report-detail-panel';

export function toReportRiskProfile(list: RiskList | undefined): ReportRiskProfileView | null {
  const profile = list?.risk_profile;
  if (!profile) return null;
  const high = profile.high_count ?? 0;
  const medium = profile.medium_count ?? 0;
  const low = profile.low_count ?? 0;
  const level: RiskLevel | undefined =
    profile.overall_level ??
    (high > 0 ? 'high' : medium > 0 ? 'medium' : low > 0 ? 'low' : undefined);
  if (!level) return null;
  return { level, high, medium, low };
}
