// useLogout — feature-хук разлогина: delegate в processes/auth-flow.logout()
// (best-effort POST /auth/logout + clear session + redirect /login — см.
// processes/auth-flow/actions.ts). Используется widgets/topbar user-menu и
// любой будущей кнопкой «Выйти» (settings, mobile-меню).
//
// Состояние `isPending` предотвращает двойной клик во время in-flight запроса
// (logout — sync-блокирующая операция для пользователя: UI переходит на /login
// только после завершения).
//
// Не мокаем шикарные тосты «Вы вышли» — явное пользовательское действие,
// обратная связь — сам факт редиректа на /login.
import { useCallback, useState } from 'react';

import { logout as coreLogout } from '@/processes/auth-flow';

export interface UseLogoutResult {
  logout: () => Promise<void>;
  isPending: boolean;
}

export function useLogout(): UseLogoutResult {
  const [isPending, setIsPending] = useState(false);

  const logout = useCallback(async (): Promise<void> => {
    if (isPending) return;
    setIsPending(true);
    try {
      await coreLogout();
    } finally {
      // Даже если coreLogout упал, всё равно сбрасываем флаг — redirect на
      // /login уже произошёл (happy-path логирования ошибок в actions.ts:
      // best-effort POST не блокирует редирект).
      setIsPending(false);
    }
  }, [isPending]);

  return { logout, isPending };
}
