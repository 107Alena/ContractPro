// @vitest-environment jsdom
//
// Smoke-тесты ContractDetailPage (FE-TASK-045): проверяют основные состояния
// (Ready/Loading/NotFound/Error/RBAC) путём мока http.get — хуки useContract
// и useVersions используют явный queryFn, поэтому setQueryDefaults/queryFn
// не переопределяет их; мокаем на уровне модуля `@/shared/api`.
//
// useEventStream тоже мокается — без мока попытается открыть EventSource
// (недоступен в jsdom).
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
    useEventStream: vi.fn(),
  };
});

import { OrchestratorError, qk } from '@/shared/api';
import { type User, useSession } from '@/shared/auth';

import { ContractDetailPage } from './ContractDetailPage';

const CONTRACT_ID = 'c1';

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

const sampleContract = {
  contract_id: CONTRACT_ID,
  title: 'Договор оказания услуг с ООО «Альфа»',
  status: 'ACTIVE' as const,
  current_version: {
    version_id: 'v2',
    version_number: 2,
    processing_status: 'READY' as const,
    processing_status_message: 'Результаты готовы',
    source_file_name: 'alpha-v2.pdf',
    origin_type: 'RE_UPLOAD' as const,
    created_at: '2026-04-16T14:20:00Z',
  },
  created_at: '2026-04-15T10:00:00Z',
  updated_at: '2026-04-16T14:20:00Z',
};

const sampleVersions = {
  items: [
    {
      version_id: 'v1',
      version_number: 1,
      processing_status: 'READY' as const,
      origin_type: 'UPLOAD' as const,
      source_file_name: 'alpha-v1.pdf',
      created_at: '2026-04-15T10:00:00Z',
    },
    sampleContract.current_version,
  ],
  total: 2,
};

function renderPage(qc: QueryClient, user: User | null = lawyer): void {
  if (user) useSession.getState().setUser(user);
  else useSession.getState().clear();
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[`/contracts/${CONTRACT_ID}`]}>
        <Routes>
          <Route path="/contracts/:id" element={<ContractDetailPage />} />
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

function mockResponses(
  contractPromise: Promise<unknown> | (() => Promise<unknown>),
  versionsPromise?: Promise<unknown> | (() => Promise<unknown>),
): void {
  getSpy.mockImplementation((url: string) => {
    const isContract = url === `/contracts/${CONTRACT_ID}`;
    const isVersions = url === `/contracts/${CONTRACT_ID}/versions`;
    if (isContract) {
      return typeof contractPromise === 'function' ? contractPromise() : contractPromise;
    }
    if (isVersions) {
      return typeof versionsPromise === 'function'
        ? versionsPromise()
        : (versionsPromise ?? Promise.resolve({ data: { items: [], total: 0 } }));
    }
    return Promise.reject(new Error(`Unexpected URL: ${url}`));
  });
}

beforeEach(() => {
  getSpy.mockReset();
});

afterEach(() => {
  cleanup();
  useSession.getState().clear();
});

describe('ContractDetailPage', () => {
  it('Ready — рендерит заголовок и ключевые блоки для LAWYER', async () => {
    const qc = makeClient();
    qc.setQueryData(qk.contracts.byId(CONTRACT_ID), sampleContract);
    qc.setQueryData(qk.contracts.versions(CONTRACT_ID), sampleVersions);

    renderPage(qc);

    await waitFor(() => {
      expect(screen.getByTestId('state-ready')).toBeDefined();
    });
    expect(screen.getByTestId('document-header')).toBeDefined();
    expect(
      screen.getByRole('heading', { level: 1, name: /Договор оказания услуг/i }),
    ).toBeDefined();
    expect(screen.getByRole('region', { name: 'Последняя проверка' })).toBeDefined();
    expect(screen.getByRole('region', { name: 'История версий' })).toBeDefined();
    expect(screen.getByRole('region', { name: 'Журнал проверок' })).toBeDefined();
    expect(screen.getByRole('region', { name: 'Ключевые риски' })).toBeDefined();
    expect(screen.getByRole('region', { name: 'Рекомендации' })).toBeDefined();
  });

  it('Loading — pending query показывает state-loading', () => {
    const qc = makeClient();
    mockResponses(new Promise(() => undefined), new Promise(() => undefined));

    renderPage(qc);

    expect(screen.getByTestId('state-loading')).toBeDefined();
  });

  it('NotFound — 404 CONTRACT_NOT_FOUND показывает inline NotFoundState', async () => {
    const qc = makeClient();
    mockResponses(
      Promise.reject(
        new OrchestratorError({
          error_code: 'CONTRACT_NOT_FOUND',
          message: 'not found',
          status: 404,
        }),
      ),
    );

    renderPage(qc);

    await waitFor(() => {
      expect(screen.getByTestId('state-not-found')).toBeDefined();
    });
    expect(screen.getByText(/Договор не найден/i)).toBeDefined();
  });

  it('Error — generic 500 показывает state-error с кнопкой retry', async () => {
    const qc = makeClient();
    mockResponses(
      Promise.reject(
        new OrchestratorError({
          error_code: 'INTERNAL_ERROR',
          message: 'Внутренняя ошибка сервера',
          status: 500,
        }),
      ),
    );

    renderPage(qc);

    await waitFor(() => {
      expect(screen.getByTestId('state-error')).toBeDefined();
    });
    expect(screen.getByTestId('retry-contract')).toBeDefined();
  });

  it('RBAC Pattern B — BUSINESS_USER не видит Ключевые риски/Рекомендации', async () => {
    const qc = makeClient();
    qc.setQueryData(qk.contracts.byId(CONTRACT_ID), sampleContract);
    qc.setQueryData(qk.contracts.versions(CONTRACT_ID), sampleVersions);

    renderPage(qc, businessUser);

    await waitFor(() => {
      expect(screen.getByTestId('document-header')).toBeDefined();
    });
    expect(screen.getByRole('region', { name: 'История версий' })).toBeDefined();
    expect(screen.queryByRole('region', { name: 'Ключевые риски' })).toBeNull();
    expect(screen.queryByRole('region', { name: 'Рекомендации' })).toBeNull();
    expect(screen.queryByRole('region', { name: 'Отклонения от политики' })).toBeNull();
  });

  it('PDF-тумблер доступен при наличии current_version; lazy виджет не в DOM до клика', async () => {
    const qc = makeClient();
    qc.setQueryData(qk.contracts.byId(CONTRACT_ID), sampleContract);
    qc.setQueryData(qk.contracts.versions(CONTRACT_ID), sampleVersions);

    renderPage(qc);

    await waitFor(() => {
      expect(screen.getByTestId('state-ready')).toBeDefined();
    });
    const toggle = screen.getByTestId('pdf-navigator-toggle') as HTMLButtonElement;
    expect(toggle.disabled).toBe(false);
    expect(screen.queryByTestId('pdf-navigator')).toBeNull();
    expect(screen.queryByTestId('pdf-navigator-suspense')).toBeNull();
  });
});
