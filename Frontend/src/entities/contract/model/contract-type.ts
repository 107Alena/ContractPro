// CONTRACT_TYPE_LABELS — отображаемые RU-лейблы типа договора (FE-TASK-058).
//
// Источник: английский LIC enum `ContractType` (12 значений, ASSUMPTION-LIC-16).
// Лейблы обратны серверной RU→EN нормализации (ORCH-TASK-055,
// ApiBackendOrchestrator/architecture/event-catalog.md §1.3). Сервер всегда
// отдаёт в ContractSummary английский enum (или null) — здесь только маппинг
// для UI (колонка «Тип», фильтр-чипы).
//
// Размещение (FSD): entities/contract — параллельно RISK_LEVEL_META в
// entities/risk (stateless reference-data по доменному полю договора, потребляется
// виджетами/страницами). В отличие от STATUS_META (shared/lib/status-view —
// cross-entity: version + contract), тип договора — атрибут только домена
// «договор», cross-entity-импорта нет, поэтому shared/lib не требуется.
// `Record<ContractType, string>` даёт compile-time проверку полноты (как
// STATUS_META / BUCKET_MAP).
import type { ContractType } from '@/shared/api';

export const CONTRACT_TYPE_LABELS: Record<ContractType, string> = {
  SERVICES: 'Услуги',
  SUPPLY: 'Поставка',
  WORK_CONTRACT: 'Подряд',
  LEASE: 'Аренда',
  NDA: 'NDA',
  SALE: 'Купля-продажа',
  LICENSE: 'Лицензия',
  AGENCY: 'Агентский',
  LOAN: 'Заём',
  INSURANCE: 'Страхование',
  EMPLOYMENT_CIVIL: 'Трудовой',
  OTHER: 'Иное',
};

// Канонический порядок типов (для filter-опций + валидации). `satisfies`
// гарантирует, что список покрывает ровно ContractType — рассинхрон с enum
// (например после регенерации openapi.d.ts) даст ошибку компиляции.
export const CONTRACT_TYPES = [
  'SERVICES',
  'SUPPLY',
  'WORK_CONTRACT',
  'LEASE',
  'NDA',
  'SALE',
  'LICENSE',
  'AGENCY',
  'LOAN',
  'INSURANCE',
  'EMPLOYMENT_CIVIL',
  'OTHER',
] as const satisfies readonly ContractType[];

/**
 * RU-лейбл типа договора или `null`, если тип не определён (поле null —
 * договор не проанализирован / тип не выявлен). Возвращает `null`, а не «—»:
 * выбор плейсхолдера остаётся за слоем отображения (data-honesty).
 */
export function contractTypeLabel(type: ContractType | null | undefined): string | null {
  return type ? CONTRACT_TYPE_LABELS[type] : null;
}
