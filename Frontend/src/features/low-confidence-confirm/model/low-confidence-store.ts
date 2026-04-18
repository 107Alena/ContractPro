// Глобальный store текущего "ожидающего подтверждения" события.
//
// Зачем глобальный, а не per-page state: SSE стрим открывается на уровне
// приложения (см. FE-TASK-042 dashboard `useEventStream(undefined)` — глобальный
// feed). type_confirmation_required может прилететь, когда пользователь не на
// странице конкретной версии (например, upload запустился и ушёл на dashboard).
// Модалка должна перехватить событие глобально.
//
// Идемпотентность (review code-architect): если backend дважды пришлёт
// type_confirmation_required для одной версии (retry/replay), второй вызов
// должен быть no-op для уже открытой/недавно закрытой версии. Поддерживаем
// LRU из 10 version_id с состоянием 'open'/'recent'. После успешного confirm
// или закрытия модалки версия попадает в 'recent' на 60s — новые события для
// этой версии игнорируются.
import { create } from 'zustand';

import type { TypeConfirmationEvent } from './types';

const RECENT_TTL_MS = 60_000;
const MAX_RECENT = 10;

interface RecentEntry {
  versionId: string;
  expiresAt: number;
}

interface State {
  current: TypeConfirmationEvent | null;
  recent: RecentEntry[];
  open: (event: TypeConfirmationEvent) => void;
  /** Закрыть модалку без подтверждения. Версия попадает в `recent` — повторные
   *  SSE-события для неё игнорируются на `RECENT_TTL_MS`. */
  dismiss: () => void;
  /** Закрыть после успешного confirm — версия в `recent` (как и при dismiss). */
  resolve: () => void;
  /** @internal Сброс state'а в тестах. */
  __reset: () => void;
}

const isRecent = (versionId: string, now: number, recent: RecentEntry[]): boolean =>
  recent.some((e) => e.versionId === versionId && e.expiresAt > now);

const pruneAndPush = (recent: RecentEntry[], versionId: string, now: number): RecentEntry[] => {
  // Сначала отбрасываем устаревшие, потом отфильтровываем дубль (если уже
  // был добавлен), затем добавляем свежий и обрезаем до MAX_RECENT (LRU).
  const filtered = recent.filter((e) => e.expiresAt > now && e.versionId !== versionId);
  filtered.push({ versionId, expiresAt: now + RECENT_TTL_MS });
  return filtered.length > MAX_RECENT ? filtered.slice(filtered.length - MAX_RECENT) : filtered;
};

export const useLowConfidenceStore = create<State>((set, get) => ({
  current: null,
  recent: [],
  open: (event) => {
    const now = Date.now();
    const { current, recent } = get();
    // Уже открыто событие для этой версии — no-op (повторный SSE retry).
    if (current && current.version_id === event.version_id) return;
    // Версия в LRU-recent — событие пришло после dismiss/resolve, игнорируем
    // до истечения TTL. SSE backend может повторно пушить retry'и.
    if (isRecent(event.version_id, now, recent)) return;
    // Если открыта модалка для ДРУГОЙ версии — текущее событие приоритетнее
    // (например, watchdog таймаут). По договорённости с product: показываем
    // последнее событие, предыдущая версия попадает в recent.
    const nextRecent = current
      ? pruneAndPush(recent, current.version_id, now)
      : recent.filter((e) => e.expiresAt > now);
    set({ current: event, recent: nextRecent });
  },
  dismiss: () => {
    const { current, recent } = get();
    if (!current) return;
    set({
      current: null,
      recent: pruneAndPush(recent, current.version_id, Date.now()),
    });
  },
  resolve: () => {
    const { current, recent } = get();
    if (!current) return;
    set({
      current: null,
      recent: pruneAndPush(recent, current.version_id, Date.now()),
    });
  },
  __reset: () => set({ current: null, recent: [] }),
}));

/** Селектор-хук для UI: текущее событие или null. */
export const useCurrentTypeConfirmation = (): TypeConfirmationEvent | null =>
  useLowConfidenceStore((s) => s.current);
