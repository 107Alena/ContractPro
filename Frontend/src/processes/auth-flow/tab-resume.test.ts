// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { sessionStore } from '@/shared/auth/session-store';

const { doRefreshMock } = vi.hoisted(() => ({
  doRefreshMock: vi.fn(async () => 'new-access'),
}));
vi.mock('./actions', () => ({
  doRefresh: doRefreshMock,
}));

import { registerTabResume, unregisterTabResume } from './tab-resume';

function fireVisibilityChange(state: 'visible' | 'hidden'): void {
  Object.defineProperty(document, 'visibilityState', { value: state, configurable: true });
  document.dispatchEvent(new Event('visibilitychange'));
}

describe('tab-resume (§5.7)', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-04-17T10:00:00.000Z'));
    doRefreshMock.mockClear();
    registerTabResume();
  });

  afterEach(() => {
    unregisterTabResume();
    sessionStore.getState().clear();
    fireVisibilityChange('visible');
    vi.useRealTimers();
  });

  it('резюм вкладки с просроченным токеном → doRefresh', () => {
    // 5s до exp — меньше 60s lead → refresh.
    sessionStore.getState().setAccess('tok', 5);
    fireVisibilityChange('hidden');
    fireVisibilityChange('visible');

    expect(doRefreshMock).toHaveBeenCalledTimes(1);
  });

  it('резюм с свежим токеном (>60s осталось) → refresh НЕ вызывается', () => {
    sessionStore.getState().setAccess('tok', 900);
    fireVisibilityChange('hidden');
    fireVisibilityChange('visible');

    expect(doRefreshMock).not.toHaveBeenCalled();
  });

  it('переход в hidden не триггерит refresh', () => {
    sessionStore.getState().setAccess('tok', 5);
    fireVisibilityChange('hidden');

    expect(doRefreshMock).not.toHaveBeenCalled();
  });

  it('без сессии (tokenExpiry=null) → refresh НЕ вызывается', () => {
    fireVisibilityChange('visible');
    expect(doRefreshMock).not.toHaveBeenCalled();
  });

  it('unregisterTabResume отписывается — события больше не триггерят refresh', () => {
    sessionStore.getState().setAccess('tok', 5);
    unregisterTabResume();
    fireVisibilityChange('hidden');
    fireVisibilityChange('visible');

    expect(doRefreshMock).not.toHaveBeenCalled();
  });

  it('повторный registerTabResume идемпотентен (нет дубль-подписки)', () => {
    registerTabResume();
    registerTabResume();
    sessionStore.getState().setAccess('tok', 5);
    fireVisibilityChange('hidden');
    fireVisibilityChange('visible');

    expect(doRefreshMock).toHaveBeenCalledTimes(1);
  });
});
