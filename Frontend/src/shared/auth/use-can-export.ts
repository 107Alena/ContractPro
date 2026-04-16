// useCanExport — условное разрешение «роль + policy» (§5.6).
// LAWYER / ORG_ADMIN — всегда true. BUSINESS_USER — по флагу
// permissions.export_enabled, который computed backend-ом (Orchestrator §6.21
// Permissions Resolver). Frontend не интерпретирует политики OPM напрямую.
import { type UserRole, useSession } from './session-store';

/**
 * Pure-проверка разрешения на экспорт. Для non-React потребителей.
 */
export function canExport(role: UserRole | undefined, exportEnabled: boolean | undefined): boolean {
  if (role === 'LAWYER' || role === 'ORG_ADMIN') return true;
  if (role === 'BUSINESS_USER') return exportEnabled === true;
  return false;
}

/**
 * React-хук: можно ли пользователю скачивать отчёты (PDF/DOCX).
 * Комбинация role + computed-флага permissions.export_enabled из /users/me.
 */
export function useCanExport(): boolean {
  const role = useSession((s) => s.user?.role);
  const exportEnabled = useSession((s) => s.user?.permissions?.export_enabled);
  return canExport(role, exportEnabled);
}
