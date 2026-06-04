// toReportRiskProfile — сворачивает RiskList (GET …/risks) в компактный
// view-model для риск-строки ReportDetailPanel (Figma 230:6).
//
// data-honesty (legal-продукт): уровень риска — ТОЛЬКО authoritative-вердикт
// бэка (LIC взвешивает счётчики, политику строгости, aggregate-score). Уровень
// из счётчиков НЕ синтезируем: иначе 1 high среди десятков low дал бы ложный
// «Высокий риск», которого бэк не выносил. Зеркалит RiskProfileCard (показывает
// бейдж уровня только при overall_level). Счётчики high/medium/low — реальные
// данные, показываем их и без вердикта. Нет ни вердикта, ни рисков → null
// (панель покажет честный плейсхолдер).
import { type RiskList } from '@/entities/result';
import { type ReportRiskProfileView } from '@/widgets/report-detail-panel';

export function toReportRiskProfile(list: RiskList | undefined): ReportRiskProfileView | null {
  const profile = list?.risk_profile;
  if (!profile) return null;
  const high = profile.high_count ?? 0;
  const medium = profile.medium_count ?? 0;
  const low = profile.low_count ?? 0;
  const level = profile.overall_level ?? null;
  if (level == null && high === 0 && medium === 0 && low === 0) return null;
  return { level, high, medium, low };
}
