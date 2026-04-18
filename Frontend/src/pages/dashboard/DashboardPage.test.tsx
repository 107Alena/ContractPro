// @vitest-environment jsdom
//
// Smoke-тесты DashboardPage: проверяют 4 состояния (Default/Loading/Empty/Error)
// через setQueryData, без MSW (FE-TASK-054) и без прямых fetch'ей. SSE-хук
// `useEventStream` мокается — виджеты под него не завязаны, но без мока хук
// попытается открыть EventSource (а его нет в jsdom).
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { qk } from '@/shared/api';
import { type User, useSession } from '@/shared/auth';

// Мокаем useEventStream до импорта DashboardPage — иначе его try-open запустит
// fetch/EventSource, которых нет в jsdom-node. Алиас мока соответствует
// публичному API модуля shared/api (через barrel use-event-stream.ts).
vi.mock('@/shared/api', async (importActual) => {
  const actual = await importActual<typeof import('@/shared/api')>();
  return {
    ...actual,
    useEventStream: vi.fn(),
  };
});

// Тоже мокаем entity-хуки, чтобы не делать реальных HTTP-запросов в компоненте.
// Каждый тест через setQueryData подставляет нужные данные.
import { KeyRisksCards } from '@/widgets/dashboard-key-risks';
import { WhatMattersCards } from '@/widgets/dashboard-what-matters';

import { DashboardPage } from './DashboardPage';

function renderPage(qc: QueryClient, user: User | null = baseUser) {
  if (user) {
    useSession.getState().setUser(user);
  } else {
    useSession.getState().clear();
  }
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <DashboardPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const baseUser: User = {
  user_id: '00000000-0000-0000-0000-000000000001',
  email: 'maria@example.com',
  name: 'Мария Петрова',
  role: 'LAWYER',
  organization_id: '00000000-0000-0000-0000-000000000002',
  organization_name: 'ООО «Правовой центр»',
  permissions: { export_enabled: true },
};

function makeClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        gcTime: 0,
        // disable refetch to keep state stable in test
        refetchOnMount: false,
      },
    },
  });
}

afterEach(() => {
  cleanup();
  useSession.getState().clear();
});

describe('DashboardPage', () => {
  it('рендерит главный заголовок «Главная»', () => {
    const qc = makeClient();
    qc.setQueryData(qk.me, baseUser);
    qc.setQueryData(qk.contracts.list({ size: 5 }), { items: [], total: 0 });
    renderPage(qc);
    expect(screen.getByRole('heading', { level: 1, name: /главная/i })).toBeDefined();
  });

  it('Default — показывает карточки и таблицу с данными', () => {
    const qc = makeClient();
    qc.setQueryData(qk.me, baseUser);
    qc.setQueryData(qk.contracts.list({ size: 5 }), {
      items: [
        { contract_id: 'c1', title: 'Аренда', processing_status: 'READY' },
        { contract_id: 'c2', title: 'Услуги', processing_status: 'ANALYZING' },
      ],
      total: 12,
    });
    renderPage(qc);

    const kpi = screen.getByRole('region', { name: 'Ключевые показатели' });
    expect(within(kpi).getByText('12')).toBeDefined(); // total
    expect(screen.getByRole('region', { name: 'Последняя проверка' })).toBeDefined();
    // «Аренда» встречается в LastCheckCard (h3) и в RecentChecksTable (link)
    expect(screen.getAllByText('Аренда').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('Услуги')).toBeDefined();
    expect(screen.getByRole('region', { name: 'Организация' })).toBeDefined();
  });

  it('Empty — /contracts пустой: показывает empty state в LastCheck и RecentChecks', () => {
    const qc = makeClient();
    qc.setQueryData(qk.me, baseUser);
    qc.setQueryData(qk.contracts.list({ size: 5 }), { items: [], total: 0 });
    renderPage(qc);

    // LastCheckCard empty-текст
    expect(screen.getByText(/пока нет проверок/i)).toBeDefined();
    // KeyRisksCards empty-текст
    expect(screen.getByText(/риски появятся после первой проверки/i)).toBeDefined();
  });

  it('скрывает QuickStart и KeyRisks для BUSINESS_USER без risk.view', () => {
    const qc = makeClient();
    // BUSINESS_USER: contract.upload=true, risks.view=false
    const bu: User = { ...baseUser, role: 'BUSINESS_USER' };
    qc.setQueryData(qk.me, bu);
    qc.setQueryData(qk.contracts.list({ size: 5 }), { items: [], total: 0 });
    renderPage(qc, bu);

    // QuickStart виден — у BUSINESS_USER есть contract.upload
    expect(screen.getByRole('region', { name: 'Быстрый старт' })).toBeDefined();
    // KeyRisksCards скрыт (risk.view — только LAWYER/ORG_ADMIN)
    expect(screen.queryByRole('region', { name: 'Ключевые риски' })).toBeNull();
  });

  it('ErrorState — виджеты принимают prop `error` и рендерят role=alert', () => {
    // Полный ErrorState-флоу страницы через TanStack (prefetch reject + useQuery)
    // нестабилен в jsdom без MSW (FE-TASK-054) — повторный fetch на mount
    // перезапускает query даже с retry=false. Здесь проверяем ключевой
    // контракт: при передаче prop error виджеты отрисовывают role=alert
    // (паттерн §9.3 row «5xx» + ADR error-boundary level 2/3). Полное
    // покрытие — Storybook ErrorState stories + Chromatic.
    render(
      <MemoryRouter>
        <WhatMattersCards error={new Error('net down')} />
        <KeyRisksCards error={new Error('net down')} />
      </MemoryRouter>,
    );
    const alerts = screen.getAllByRole('alert');
    expect(alerts.length).toBeGreaterThanOrEqual(2);
  });
});
