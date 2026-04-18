// Zustand layout UI-store — cross-widget client state для навигационной shell.
// Архитектура: §4.1 (UI-стейт через Zustand slices), §8.3 (sidebar collapsed / drawer)
// — Frontend/architecture/high-architecture.md.
//
// Поднят в shared/layout/, а не внутри widgets/sidebar-navigation, потому что
// state шарится между sidebar-navigation (toggle-кнопка) и topbar (бургер для
// mobileDrawerOpen) — FSD v2 запрещает cross-widget import. См. ADR по FSD
// boundaries в Frontend/eslint.config.js (`widgets → sliceSame('widgets')`).
//
// Persistence: только `sidebarCollapsed` (partialize) — пользователь ожидает,
// что свёрнутая боковая панель остаётся свёрнутой после F5. `mobileDrawerOpen`
// НЕ персистится: всегда false на mount — иначе drawer откроется после reload
// на mobile и перекроет контент.
import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';

export interface LayoutState {
  sidebarCollapsed: boolean;
  mobileDrawerOpen: boolean;
  toggleSidebar: () => void;
  setSidebarCollapsed: (value: boolean) => void;
  openMobileDrawer: () => void;
  closeMobileDrawer: () => void;
  setMobileDrawerOpen: (value: boolean) => void;
}

export const LAYOUT_STORAGE_KEY = 'cp:layout:v1';

export const useLayoutStore = create<LayoutState>()(
  persist(
    (set) => ({
      sidebarCollapsed: false,
      mobileDrawerOpen: false,
      toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
      setSidebarCollapsed: (value) => set({ sidebarCollapsed: value }),
      openMobileDrawer: () => set({ mobileDrawerOpen: true }),
      closeMobileDrawer: () => set({ mobileDrawerOpen: false }),
      setMobileDrawerOpen: (value) => set({ mobileDrawerOpen: value }),
    }),
    {
      name: LAYOUT_STORAGE_KEY,
      storage: createJSONStorage(() => localStorage),
      // Только sidebarCollapsed уходит в localStorage; mobileDrawerOpen
      // всегда читается дефолтным (false) на свежий mount.
      partialize: (state) => ({ sidebarCollapsed: state.sidebarCollapsed }),
      version: 1,
    },
  ),
);

// Vanilla-alias для non-React потребителей (кастомные imperative-хэндлеры).
// Совпадает с паттерном sessionStore — Zustand v4 hook сам по себе store.
export const layoutStore = useLayoutStore;

// Узкие селекторы: ре-рендер только при изменении конкретного поля.
export const useSidebarCollapsed = (): boolean => useLayoutStore((state) => state.sidebarCollapsed);
export const useMobileDrawerOpen = (): boolean => useLayoutStore((state) => state.mobileDrawerOpen);
