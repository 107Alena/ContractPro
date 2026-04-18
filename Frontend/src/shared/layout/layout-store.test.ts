// @vitest-environment jsdom
// Unit-тесты для layout-store: partialize-persist, mobileDrawer никогда не персистится,
// toggle-логика, rehydration.
import { act } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { LAYOUT_STORAGE_KEY, useLayoutStore } from './layout-store';

function resetStore(): void {
  act(() => {
    useLayoutStore.setState({ sidebarCollapsed: false, mobileDrawerOpen: false });
  });
  localStorage.clear();
}

beforeEach(resetStore);
afterEach(resetStore);

describe('layout-store', () => {
  it('дефолтное состояние: expanded + mobile closed', () => {
    const state = useLayoutStore.getState();
    expect(state.sidebarCollapsed).toBe(false);
    expect(state.mobileDrawerOpen).toBe(false);
  });

  it('toggleSidebar переключает collapsed', () => {
    useLayoutStore.getState().toggleSidebar();
    expect(useLayoutStore.getState().sidebarCollapsed).toBe(true);
    useLayoutStore.getState().toggleSidebar();
    expect(useLayoutStore.getState().sidebarCollapsed).toBe(false);
  });

  it('setSidebarCollapsed(true) фиксирует значение', () => {
    useLayoutStore.getState().setSidebarCollapsed(true);
    expect(useLayoutStore.getState().sidebarCollapsed).toBe(true);
  });

  it('openMobileDrawer / closeMobileDrawer', () => {
    useLayoutStore.getState().openMobileDrawer();
    expect(useLayoutStore.getState().mobileDrawerOpen).toBe(true);
    useLayoutStore.getState().closeMobileDrawer();
    expect(useLayoutStore.getState().mobileDrawerOpen).toBe(false);
  });

  it('persist сохраняет только sidebarCollapsed (partialize)', async () => {
    useLayoutStore.getState().setSidebarCollapsed(true);
    useLayoutStore.getState().openMobileDrawer();
    // Forsirovat' synchro-flush persist-middleware'a.
    await useLayoutStore.persist.rehydrate();

    const raw = localStorage.getItem(LAYOUT_STORAGE_KEY);
    expect(raw).toBeTruthy();
    const parsed = JSON.parse(raw as string) as {
      state: Record<string, unknown>;
      version: number;
    };
    expect(parsed.state).toEqual({ sidebarCollapsed: true });
    expect(parsed.state).not.toHaveProperty('mobileDrawerOpen');
  });

  it('rehydrate восстанавливает sidebarCollapsed из localStorage', async () => {
    localStorage.setItem(
      LAYOUT_STORAGE_KEY,
      JSON.stringify({ state: { sidebarCollapsed: true }, version: 1 }),
    );
    await useLayoutStore.persist.rehydrate();
    expect(useLayoutStore.getState().sidebarCollapsed).toBe(true);
    // mobileDrawerOpen сбрасывается в дефолт — не в localStorage.
    expect(useLayoutStore.getState().mobileDrawerOpen).toBe(false);
  });
});
