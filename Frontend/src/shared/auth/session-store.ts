// Zustand session-store — access-token in memory, user profile, expiry.
// Архитектура: §5.2 (хранение токенов), §5.3 (silent-refresh таймер),
// §5.6 (RBAC-селекторы), §7.2 (non-React доступ через `.getState()`),
// ADR-FE-03 (Access in-memory) — Frontend/architecture/high-architecture.md.
//
// Стор НЕ сериализуется (без persist-middleware): перезагрузка страницы
// обнуляет access, refresh-flow (FE-TASK-027) восстанавливает через
// POST /auth/refresh (refresh-token → Secure cookie; fallback — sessionStorage,
// ADR-FE-03).
import { create } from 'zustand';

import type { components } from '@/shared/api/openapi';

export type User = components['schemas']['UserProfile'];
export type UserRole = User['role'];

export interface SessionState {
  accessToken: string | null;
  user: User | null;
  // Абсолютный epoch-ms истечения access-токена. Упрощает таймер refresh
  // за 60s до exp (§5.3): `setTimeout(refresh, tokenExpiry - Date.now() - 60_000)`.
  tokenExpiry: number | null;
  setAccess: (token: string, expiresIn: number) => void;
  setUser: (user: User) => void;
  clear: () => void;
}

export const useSession = create<SessionState>((set) => ({
  accessToken: null,
  user: null,
  tokenExpiry: null,
  setAccess: (token, expiresIn) =>
    set({ accessToken: token, tokenExpiry: Date.now() + expiresIn * 1000 }),
  setUser: (user) => set({ user }),
  clear: () => set({ accessToken: null, user: null, tokenExpiry: null }),
}));

// Vanilla-alias для non-React потребителей: axios-интерсептор (§7.2),
// SSE-wrapper (§7.7), auth-flow-таймер (§5.3). Zustand v4 hook сам по себе —
// store с методами `.getState/.setState/.subscribe`.
export const sessionStore = useSession;

export const useAccessToken = (): string | null => useSession((s) => s.accessToken);
export const useRole = (): UserRole | undefined => useSession((s) => s.user?.role);
export const useIsAuthenticated = (): boolean => useSession((s) => s.accessToken !== null);
