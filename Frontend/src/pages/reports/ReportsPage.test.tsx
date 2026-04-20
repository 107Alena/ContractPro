// @vitest-environment jsdom
//
// Smoke-тесты ReportsPage (FE-TASK-048): основные состояния (Default / Loading
// / Empty / Error / ExpiredLinkBanner / Row-select → Detail-panel).
// Паттерн тот же, что в ContractsListPage.test.tsx — мокаем http.get.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const { getSpy } = vi.hoisted(() => ({ getSpy: vi.fn() }));

vi.mock('@/shared/api', async (importActual) => {
  const actual = await importActual<typeof import('@/shared/api')>();
  return {
    ...actual,
    http: { get: getSpy },
  };
});

import { OrchestratorError } from '@/shared/api';
import { type User, useSession } from '@/shared/auth';

import { ReportsPage } from './ReportsPage';

const lawyer: User = {
  user_id: '00000000-0000-0000-0000-000000000001',
  email: 'maria@example.com',
  name: 'Мария Петрова',
  role: 'LAWYER',
  organization_id: '00000000-0000-0000-0000-000000000002',
  organization_name: 'ООО «Правовой центр»',
  permissions: { export_enabled: true },
};

const sample = {
  items: [
    {
      contract_id: 'c1',
      title: 'Договор оказания услуг',
      status: 'ACTIVE' as const,
      current_version_number: 2,
      processing_status: 'READY' as const,
      updated_at: '2026-04-16T14:20:00Z',
    },
    {
      contract_id: 'c2',
      title: 'NDA с ООО «Бета»',
      status: 'ACTIVE' as const,
      current_version_number: 1,
      processing_status: 'READY' as const,
      updated_at: '2026-04-10T10:00:00Z',
    },
    {
      contract_id: 'c3',
      title: 'Договор аренды (с предупреждениями)',
      status: 'ACTIVE' as const,
      current_version_number: 1,
      processing_status: 'PARTIALLY_FAILED' as const,
      updated_at: '2026-04-05T09:00:00Z',
    },
  ],
  total: 3,
  page: 1,
  size: 25,
};

function renderPage(qc: QueryClient, user: User | null = lawyer, initialEntry = '/reports'): void {
  if (user) useSession.getState().setUser(user);
  else useSession.getState().clear();
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/reports" element={<ReportsPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function makeClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, retryDelay: 0, gcTime: 0, refetchOnMount: false },
    },
  });
}

beforeEach(() => {
  getSpy.mockReset();
});

afterEach(() => {
  cleanup();
  useSession.getState().clear();
});

describe('ReportsPage', () => {
  it('Default — рендерит metrics + таблицу + по умолчанию фильтр READY', async () => {
    getSpy.mockResolvedValue({ data: sample });
    const qc = makeClient();
    renderPage(qc);

    await waitFor(() => {
      expect(screen.getByText('Договор оказания услуг')).toBeInTheDocument();
    });
    expect(screen.getByTestId('page-reports')).toBeInTheDocument();
    expect(screen.getByTestId('reports-metrics')).toBeInTheDocument();
    expect(screen.getByTestId('reports-table')).toBeInTheDocument();
    // PARTIALLY_FAILED отфильтрован клиентом (state=READY default) — не виден.
    expect(screen.queryByText('Договор аренды (с предупреждениями)')).toBeNull();
  });

  it('Loading — pending query → спиннер в таблице', () => {
    getSpy.mockImplementation(() => new Promise(() => undefined));
    const qc = makeClient();
    renderPage(qc);

    expect(screen.getByTestId('reports-table-loading')).toBeInTheDocument();
  });

  it('Empty — пустой ответ → default empty-state', async () => {
    getSpy.mockResolvedValue({ data: { items: [], total: 0, page: 1, size: 25 } });
    const qc = makeClient();
    renderPage(qc);

    await waitFor(() => {
      expect(screen.getByTestId('reports-table-empty')).toBeInTheDocument();
    });
  });

  it('Error — 5xx → error body + alert', async () => {
    getSpy.mockRejectedValue(
      new OrchestratorError({
        error_code: 'INTERNAL_ERROR',
        message: 'Внутренняя ошибка сервера',
        status: 500,
      }),
    );
    const qc = makeClient();
    renderPage(qc);

    await waitFor(() => {
      expect(screen.getByTestId('reports-table-error')).toBeInTheDocument();
    });
    expect(screen.getByTestId('reports-error')).toBeInTheDocument();
  });

  it('URL search → уходит серверу как params.search', async () => {
    getSpy.mockResolvedValue({ data: sample });
    const qc = makeClient();
    renderPage(qc, lawyer, '/reports?q=аренда');

    await waitFor(() => {
      expect(screen.getByTestId('reports-table')).toBeInTheDocument();
    });
    const lastCall = getSpy.mock.calls.at(-1);
    expect(lastCall?.[0]).toBe('/contracts');
    const params = (lastCall?.[1] as { params?: { search?: string; status?: string } })?.params;
    expect(params?.search).toBe('аренда');
    expect(params?.status).toBe('ACTIVE');
  });

  it('ExpiredLinkBanner — ?share=expired → баннер виден, после Скрыть → query очищается', async () => {
    getSpy.mockResolvedValue({ data: sample });
    const qc = makeClient();
    renderPage(qc, lawyer, '/reports?share=expired');

    await waitFor(() => {
      expect(screen.getByTestId('expired-link-banner')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('expired-link-banner-dismiss'));
    await waitFor(() => {
      expect(screen.queryByTestId('expired-link-banner')).toBeNull();
    });
  });

  it('Row select → открывается ReportDetailPanel; close → панель пропадает', async () => {
    getSpy.mockResolvedValue({ data: sample });
    const qc = makeClient();
    renderPage(qc);

    await waitFor(() => {
      expect(screen.getByText('Договор оказания услуг')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTestId('reports-table-row-c1'));
    expect(await screen.findByTestId('report-detail-panel')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('report-detail-panel-close'));
    await waitFor(() => {
      expect(screen.queryByTestId('report-detail-panel')).toBeNull();
    });
  });

  it('Фильтр state=PARTIALLY_FAILED — показывает контракты с предупреждениями', async () => {
    getSpy.mockResolvedValue({ data: sample });
    const qc = makeClient();
    renderPage(qc, lawyer, '/reports?state=PARTIALLY_FAILED');

    await waitFor(() => {
      expect(screen.getByText('Договор аренды (с предупреждениями)')).toBeInTheDocument();
    });
    expect(screen.queryByText('Договор оказания услуг')).toBeNull();
  });
});
