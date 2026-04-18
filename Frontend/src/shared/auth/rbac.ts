// RBAC: PERMISSIONS таблица + useCan-хук + pure `can()` для non-React.
// Архитектура: §5.5 (таблица PERMISSIONS), §5.6 (guards и правила server-truth)
// — Frontend/architecture/high-architecture.md.
//
// Клиентский RBAC — только UX-защита: Backend остаётся истиной, 403
// PERMISSION_DENIED всегда показывается как экран «Недостаточно прав».
import { type UserRole, useSession } from './session-store';

// `as const` сохраняет узкие literal-ключи (для `keyof typeof`).
// `satisfies Record<string, readonly UserRole[]>` валидирует значения
// без потери узких ключей — опечатка в роли даст compile-error на таблице,
// а не на месте использования.
export const PERMISSIONS = {
  'contract.upload': ['LAWYER', 'BUSINESS_USER', 'ORG_ADMIN'],
  'contract.archive': ['LAWYER', 'ORG_ADMIN'],
  'risks.view': ['LAWYER', 'ORG_ADMIN'],
  'summary.view': ['LAWYER', 'BUSINESS_USER', 'ORG_ADMIN'],
  'recommendations.view': ['LAWYER', 'ORG_ADMIN'],
  'comparison.run': ['LAWYER', 'ORG_ADMIN'],
  'version.recheck': ['LAWYER', 'ORG_ADMIN'],
  // Подтверждение типа договора (FR-2.1.3) — модалка LowConfidenceConfirm.
  // BUSINESS_USER не видит модалку и не может подтвердить тип, даже если SSE
  // event докатился (provider в feature low-confidence-confirm проверяет это
  // на уровне регистрации listener'а — для бизнес-пользователя бридж noop).
  'version.confirm-type': ['LAWYER', 'ORG_ADMIN'],
  'admin.policies': ['ORG_ADMIN'],
  'admin.checklists': ['ORG_ADMIN'],
  'audit.view': ['ORG_ADMIN'],
  // export.download — БЕЗУСЛОВНЫЙ role-whitelist. BUSINESS_USER получает
  // условный доступ через useCanExport() (role + permissions.export_enabled),
  // см. §5.6 + Orchestrator high-architecture §6.21 Permissions Resolver.
  'export.download': ['LAWYER', 'ORG_ADMIN'],
} as const satisfies Record<string, readonly UserRole[]>;

export type Permission = keyof typeof PERMISSIONS;

/**
 * Pure-проверка разрешения. Использовать из non-React потребителей
 * (axios-interceptor, SSE-wrapper, query-guards) — доступ к роли через
 * `sessionStore.getState().user?.role`.
 */
export function can(role: UserRole | undefined, permission: Permission): boolean {
  if (!role) return false;
  // Присваивание к broader `readonly UserRole[]` расширяет literal-tuple
  // (например `readonly ['ORG_ADMIN']`) — иначе `.includes(role: UserRole)`
  // не сходится по типам при TS strict + `as const satisfies`.
  const allowed: readonly UserRole[] = PERMISSIONS[permission];
  return allowed.includes(role);
}

/**
 * React-хук проверки разрешения: `useCan('risks.view')` → boolean.
 * Реактивно пересчитывается при смене роли в session-store.
 */
export function useCan(permission: Permission): boolean {
  const role = useSession((s) => s.user?.role);
  return can(role, permission);
}
