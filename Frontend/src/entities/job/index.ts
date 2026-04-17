// Job entity — re-export доменных типов Job. Полная доменная сущность
// (progress-шаги, DLQ, lifecycle) появится позже. Для FE-TASK-015 достаточно
// re-export'а `StatusEvent`/`UserProcessingStatus` из shared/api —
// владелец контракта SSE — транспортный слой (§7.7), entity просто делает
// payload удобно доступным на уровне features/widgets/pages по §20.2.
//
// Причина владения в shared/api, а не здесь: FSD boundaries запрещают
// `shared -> entities` импорт; sse.ts сам лежит в shared/api, поэтому
// определения типов обязаны жить там.
export type { StatusEvent, UserProcessingStatus } from '@/shared/api/sse-events';
