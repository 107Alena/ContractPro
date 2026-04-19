// RiskLevel — уровень риска (FR-3.3.1, ТЗ-1 §4.3). Формат совпадает с
// полями OpenAPI: `RiskProfile.overall_level`, `Risk.level`
// (shared/api/openapi.d.ts:693/701) — «high» | «medium» | «low».
//
// RISK_LEVEL_META — источник истины для отображения риска в UI:
// - `label` — пользовательский лейбл из ТЗ-1 (высокий/средний/низкий риск).
// - `tone` — визуальный tone shared/ui Badge (danger/warning/success),
//   согласован с токенами `--color-risk-high/medium/low` (tokens.css:18-20).
// - `legend` — короткое объяснение уровня, показывается в tooltip'е
//   (spec: «tooltip с legend», tasks.json FE-TASK-024). Источник: ТЗ-1 §4.3
//   и §5.1 (FR-3.3.1 / FR-5.1.1).
import type { BadgeProps } from '@/shared/ui';

export type RiskLevel = 'high' | 'medium' | 'low';

export interface RiskLevelMeta {
  label: string;
  tone: NonNullable<BadgeProps['variant']>;
  legend: string;
}

export const RISK_LEVEL_META: Record<RiskLevel, RiskLevelMeta> = {
  high: {
    label: 'Высокий риск',
    tone: 'danger',
    legend:
      'Блокирующие расхождения: нарушение императивных норм ГК РФ или ' +
      'существенные финансовые угрозы. Требуют переговоров или отказа от сделки.',
  },
  medium: {
    label: 'Средний риск',
    tone: 'warning',
    legend:
      'Условия под вопросом: отклонения от рекомендуемых практик или политик организации. ' +
      'Требуют дополнительной проверки и согласования.',
  },
  low: {
    label: 'Низкий риск',
    tone: 'success',
    legend:
      'Редакционные замечания: уточнения формулировок, ' +
      'не влияющие на существенные условия договора.',
  },
};

export const RISK_LEVELS = ['high', 'medium', 'low'] as const satisfies readonly RiskLevel[];

export function riskLevelMeta(level: RiskLevel): RiskLevelMeta {
  return RISK_LEVEL_META[level];
}
