// @vitest-environment jsdom
//
// Smoke-тесты ContractsListPage (FE-TASK-044): проверяют основные состояния
// (Default/Loading/Empty/Error/RBAC) через мок `http.get` — хуки useContracts
// и useArchive/useDelete мокаем на уровне модуля `@/shared/api` (http.get
// покрывает и useContracts, и useArchiveContract/useDeleteContract через
// их собственные http-инстансы — но у них своя обёртка getHttpInstance).
//
// Здесь достаточно мокать useContracts (через http.get в shared/api).
// Мутации не тестируем — покрыто в features/contract-archive|contract-delete.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
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

import { ContractsListPage } from './ContractsListPage';

const lawyer: User = {
  user_id: '00000000-0000-0000-0000-000000000001',
  email: 'maria@example.com',
  name: 'Мария Петрова',
  role: 'LAWYER',
  organization_id: '00000000-0000-0000-0000-000000000002',
  organization_name: 'ООО «Правовой центр»',
  permissions: { export_enabled: true },
};
const businessUser: User = { ...lawyer, role: 'BUSINESS_USER' };

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
      status: 'ARCHIVED' as const,
      current_version_number: 1,
      processing_status: 'READY' as const,
      updated_at: '2026-04-10T10:00:00Z',
    },
  ],
  total: 2,
  page: 1,
  size: 25,
};

function renderPage(
  qc: QueryClient,
  user: User | null = lawyer,
  initialEntry = '/contracts',
): void {
  if (user) useSession.getState().setUser(user);
  else useSession.getState().clear();
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initialEntry]}>
        <Routes>
          <Route path="/contracts" element={<ContractsListPage />} />
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

describe('ContractsListPage', () => {
  it('Default — рендерит metrics + таблицу + ссылки', async () => {
    getSpy.mockResolvedValue({ data: sample });
    const qc = makeClient();

    renderPage(qc);

    await waitFor(() => {
      expect(screen.getByText('Договор оказания услуг')).toBeInTheDocument();
    });
    expect(screen.getByTestId('page-contracts-list')).toBeInTheDocument();
    expect(screen.getByTestId('contracts-metrics-strip')).toBeInTheDocument();
    expect(screen.getByTestId('contracts-list-new')).toBeInTheDocument();
    expect(screen.getByTestId('documents-table')).toBeInTheDocument();
  });

  it('Loading — pending query → спиннер в таблице', async () => {
    getSpy.mockImplementation(() => new Promise(() => undefined));
    const qc = makeClient();

    renderPage(qc);

    expect(screen.getByTestId('documents-table-loading')).toBeInTheDocument();
  });

  it('Empty — пустой ответ сервера → CTA загрузки', async () => {
    getSpy.mockResolvedValue({ data: { items: [], total: 0, page: 1, size: 25 } });
    const qc = makeClient();

    renderPage(qc);

    await waitFor(() => {
      expect(screen.getByTestId('documents-table-empty')).toBeInTheDocument();
    });
    // PaginationControls НЕ рендерится при total=0.
    expect(screen.queryByText(/Записей нет/i)).toBeNull();
  });

  it('Error — 5xx → таблица показывает error-state + alert внизу', async () => {
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
      expect(screen.getByTestId('documents-table-error')).toBeInTheDocument();
    });
    expect(screen.getByTestId('contracts-list-error')).toBeInTheDocument();
  });

  it('RBAC Pattern B — BUSINESS_USER не видит кнопки архивации/удаления в строках', async () => {
    getSpy.mockResolvedValue({ data: sample });
    const qc = makeClient();

    renderPage(qc, businessUser);

    await waitFor(() => {
      expect(screen.getByText('Договор оказания услуг')).toBeInTheDocument();
    });
    expect(screen.queryByTestId('row-actions-c1')).toBeNull();
    expect(screen.queryByTestId('row-archive-c1')).toBeNull();
    expect(screen.queryByTestId('row-delete-c1')).toBeNull();
  });

  it('RBAC Pattern B — LAWYER видит кнопки архивации/удаления', async () => {
    getSpy.mockResolvedValue({ data: sample });
    const qc = makeClient();

    renderPage(qc, lawyer);

    await waitFor(() => {
      expect(screen.getByTestId('row-archive-c1')).toBeInTheDocument();
    });
    expect(screen.getByTestId('row-delete-c1')).toBeInTheDocument();
  });

  it('URL search param → попадает в params и отображается в SearchInput', async () => {
    getSpy.mockResolvedValue({ data: sample });
    const qc = makeClient();

    renderPage(qc, lawyer, '/contracts?q=аренда');

    await waitFor(() => {
      expect(screen.getByTestId('documents-table')).toBeInTheDocument();
    });
    const input = screen.getByRole('searchbox') as HTMLInputElement;
    expect(input.value).toBe('аренда');
  });

  it('URL status filter → серверу уходит status=ACTIVE', async () => {
    getSpy.mockResolvedValue({ data: sample });
    const qc = makeClient();

    renderPage(qc, lawyer, '/contracts?status=ACTIVE');

    await waitFor(() => {
      expect(screen.getByTestId('documents-table')).toBeInTheDocument();
    });
    // Проверяем, что http.get вызвался с params.status=ACTIVE.
    const lastCall = getSpy.mock.calls.at(-1);
    expect(lastCall?.[0]).toBe('/contracts');
    expect((lastCall?.[1] as { params?: { status?: string } })?.params?.status).toBe('ACTIVE');
  });

  it('Pagination — total>size → PaginationControls рендерится', async () => {
    getSpy.mockResolvedValue({
      data: { items: sample.items, total: 80, page: 1, size: 25 },
    });
    const qc = makeClient();

    renderPage(qc, lawyer);

    await waitFor(() => {
      expect(screen.getByText('Договор оказания услуг')).toBeInTheDocument();
    });
    // PaginationControls содержит «из N записей».
    expect(screen.getByText(/из\s80/)).toBeInTheDocument();
  });
});
