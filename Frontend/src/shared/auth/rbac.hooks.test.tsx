// @vitest-environment jsdom
// Hook-тесты useCan/useCanExport + компонентный <Can> — требуют DOM.
// docblock выше переключает environment локально, глобальный vitest.config.ts
// остаётся на node (минимальный scope, FE-TASK-053 — полный stack).
import { cleanup, render, renderHook, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { Can } from './can';
import { useCan } from './rbac';
import { type User, useSession } from './session-store';
import { useCanExport } from './use-can-export';

const baseUser: User = {
  user_id: '00000000-0000-0000-0000-000000000001',
  email: 'u@example.com',
  name: 'Тест',
  role: 'LAWYER',
  organization_id: '00000000-0000-0000-0000-000000000002',
  organization_name: 'Тест Орг',
  permissions: { export_enabled: false },
};

beforeEach(() => {
  useSession.getState().clear();
});

afterEach(() => {
  cleanup();
  useSession.getState().clear();
});

describe('useCan', () => {
  it('без роли возвращает false', () => {
    const { result } = renderHook(() => useCan('risks.view'));
    expect(result.current).toBe(false);
  });

  it('LAWYER → risks.view=true, admin.policies=false', () => {
    useSession.setState({ user: { ...baseUser, role: 'LAWYER' } });
    expect(renderHook(() => useCan('risks.view')).result.current).toBe(true);
    expect(renderHook(() => useCan('admin.policies')).result.current).toBe(false);
  });

  it('BUSINESS_USER → summary.view=true, risks.view=false', () => {
    useSession.setState({ user: { ...baseUser, role: 'BUSINESS_USER' } });
    expect(renderHook(() => useCan('summary.view')).result.current).toBe(true);
    expect(renderHook(() => useCan('risks.view')).result.current).toBe(false);
    expect(renderHook(() => useCan('export.download')).result.current).toBe(false);
  });

  it('ORG_ADMIN → admin.policies=true, audit.view=true', () => {
    useSession.setState({ user: { ...baseUser, role: 'ORG_ADMIN' } });
    expect(renderHook(() => useCan('admin.policies')).result.current).toBe(true);
    expect(renderHook(() => useCan('audit.view')).result.current).toBe(true);
    expect(renderHook(() => useCan('contract.upload')).result.current).toBe(true);
  });
});

describe('useCanExport (§5.6)', () => {
  it('без роли — false', () => {
    const { result } = renderHook(() => useCanExport());
    expect(result.current).toBe(false);
  });

  it('LAWYER — true при export_enabled=false (роль-приоритет)', () => {
    useSession.setState({
      user: { ...baseUser, role: 'LAWYER', permissions: { export_enabled: false } },
    });
    expect(renderHook(() => useCanExport()).result.current).toBe(true);
  });

  it('ORG_ADMIN — true всегда', () => {
    useSession.setState({
      user: { ...baseUser, role: 'ORG_ADMIN', permissions: { export_enabled: false } },
    });
    expect(renderHook(() => useCanExport()).result.current).toBe(true);
  });

  it('BUSINESS_USER + export_enabled=true → true', () => {
    useSession.setState({
      user: { ...baseUser, role: 'BUSINESS_USER', permissions: { export_enabled: true } },
    });
    expect(renderHook(() => useCanExport()).result.current).toBe(true);
  });

  it('BUSINESS_USER + export_enabled=false → false (default ORCH_OPM_FALLBACK)', () => {
    useSession.setState({
      user: { ...baseUser, role: 'BUSINESS_USER', permissions: { export_enabled: false } },
    });
    expect(renderHook(() => useCanExport()).result.current).toBe(false);
  });
});

describe('<Can>', () => {
  it('рендерит children при наличии прав', () => {
    useSession.setState({ user: { ...baseUser, role: 'LAWYER' } });
    render(
      <Can I="risks.view">
        <span>Риски видны</span>
      </Can>,
    );
    expect(screen.getByText('Риски видны')).toBeTruthy();
  });

  it('без прав и без fallback — ничего не рендерит', () => {
    useSession.setState({ user: { ...baseUser, role: 'BUSINESS_USER' } });
    const { container } = render(
      <Can I="risks.view">
        <span>Секретно</span>
      </Can>,
    );
    expect(container.textContent).toBe('');
  });

  it('без прав + fallback — рендерит fallback', () => {
    useSession.setState({ user: { ...baseUser, role: 'BUSINESS_USER' } });
    render(
      <Can I="admin.policies" fallback={<span>Нет доступа</span>}>
        <span>Админка</span>
      </Can>,
    );
    expect(screen.getByText('Нет доступа')).toBeTruthy();
    expect(screen.queryByText('Админка')).toBeNull();
  });
});
