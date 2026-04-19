// @vitest-environment jsdom
//
// Smoke-тесты ResultPage (FE-TASK-046): 8 состояний Figma (§17.4) + RBAC Pattern B.
// useContract/useResults используют http.get — мокаем `@/shared/api`.
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
    useEventStream: vi.fn(),
  };
});

import { OrchestratorError, qk } from '@/shared/api';
import { type User, useSession } from '@/shared/auth';

import { ResultPage } from './ResultPage';

const CONTRACT_ID = 'c1';
const VERSION_ID = 'v2';

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

function makeContract(status: string, message?: string): unknown {
  return {
    contract_id: CONTRACT_ID,
    title: 'Договор оказания услуг с ООО «Альфа»',
    status: 'ACTIVE',
    current_version: {
      version_id: VERSION_ID,
      version_number: 2,
      processing_status: status,
      processing_status_message: message,
      source_file_name: 'alpha-v2.pdf',
      origin_type: 'UPLOAD',
      created_at: '2026-04-16T14:20:00Z',
    },
    created_at: '2026-04-15T10:00:00Z',
    updated_at: '2026-04-16T14:20:00Z',
  };
}

const readyContract = makeContract('READY');
const processingContract = makeContract('PROCESSING', 'Анализ договора');
const awaitingContract = makeContract(
  'AWAITING_USER_INPUT',
  'Требуется подтверждение типа договора',
);
const failedContract = makeContract('FAILED', 'Не удалось распознать файл');
const rejectedContract = makeContract('REJECTED', 'Файл повреждён');
const partiallyFailedContract = makeContract('PARTIALLY_FAILED', 'Отчёт сформирован частично');

const fullResults = {
  version_id: VERSION_ID,
  status: 'READY' as const,
  contract_type: { contract_type: 'Услуги', confidence: 0.92 },
  key_parameters: {
    parties: ['ООО «Альфа»', 'ООО «Бета»'],
    subject: 'Оказание консалтинговых услуг',
    price: '1 200 000 ₽',
    duration: '12 месяцев',
  },
  risk_profile: { overall_level: 'medium', high_count: 1, medium_count: 2, low_count: 4 },
  risks: [
    {
      id: 'r1',
      level: 'high' as const,
      description: 'Штраф без ограничений',
      clause_ref: '5.3',
      legal_basis: 'ГК РФ ст. 330',
    },
    {
      id: 'r2',
      level: 'medium' as const,
      description: 'Срок оплаты не согласован',
      clause_ref: '3.1',
      legal_basis: 'Внутренняя политика организации',
    },
  ],
  recommendations: [
    {
      risk_id: 'r1',
      original_text: 'штраф 10% от суммы',
      recommended_text: 'штраф не более 0,1% за каждый день просрочки',
      explanation: 'Ограничивает размер штрафа',
    },
  ],
  summary: 'Договор оказания консалтинговых услуг на 12 месяцев за 1 200 000 ₽.',
  aggregate_score: { score: 0.62, label: 'Средний риск' },
};

const businessResults = {
  version_id: VERSION_ID,
  status: 'READY' as const,
  key_parameters: fullResults.key_parameters,
  summary: fullResults.summary,
  aggregate_score: fullResults.aggregate_score,
};

function renderPage(qc: QueryClient, user: User | null = lawyer): void {
  if (user) useSession.getState().setUser(user);
  else useSession.getState().clear();
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[`/contracts/${CONTRACT_ID}/versions/${VERSION_ID}/result`]}>
        <Routes>
          <Route path="/contracts/:id/versions/:vid/result" element={<ResultPage />} />
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

describe('ResultPage', () => {
  it('Loading — contract-query в pending', () => {
    const qc = makeClient();
    getSpy.mockImplementation(() => new Promise(() => undefined));
    renderPage(qc);
    expect(screen.getByTestId('state-loading')).toBeDefined();
  });

  it('NotFound — 404 CONTRACT_NOT_FOUND → inline NotFoundState', async () => {
    const qc = makeClient();
    getSpy.mockRejectedValue(
      new OrchestratorError({
        error_code: 'CONTRACT_NOT_FOUND',
        message: 'not found',
        status: 404,
      }),
    );
    renderPage(qc);
    await waitFor(() => {
      expect(screen.getByTestId('state-not-found')).toBeDefined();
    });
  });

  it('Processing — status=PROCESSING → ProcessingBanner', async () => {
    const qc = makeClient();
    qc.setQueryData(qk.contracts.byId(CONTRACT_ID), processingContract);
    renderPage(qc);
    await waitFor(() => {
      expect(screen.getByTestId('state-processing')).toBeDefined();
    });
  });

  it('AwaitingInput — status=AWAITING_USER_INPUT → CTA для подтверждения', async () => {
    const qc = makeClient();
    qc.setQueryData(qk.contracts.byId(CONTRACT_ID), awaitingContract);
    renderPage(qc);
    await waitFor(() => {
      expect(screen.getByTestId('state-awaiting')).toBeDefined();
    });
    expect(screen.getByTestId('awaiting-confirm-link')).toBeDefined();
  });

  it('Failed — status=FAILED → FailedState с RecheckButton', async () => {
    const qc = makeClient();
    qc.setQueryData(qk.contracts.byId(CONTRACT_ID), failedContract);
    renderPage(qc);
    await waitFor(() => {
      expect(screen.getByTestId('state-failed')).toBeDefined();
    });
    expect(screen.getAllByTestId('recheck-button').length).toBeGreaterThan(0);
  });

  it('Rejected — status=REJECTED → RejectedState', async () => {
    const qc = makeClient();
    qc.setQueryData(qk.contracts.byId(CONTRACT_ID), rejectedContract);
    renderPage(qc);
    await waitFor(() => {
      expect(screen.getByTestId('state-rejected')).toBeDefined();
    });
    expect(screen.getByTestId('rejected-replace-link')).toBeDefined();
  });

  it('Ready (LAWYER) — все секции видны', async () => {
    const qc = makeClient();
    qc.setQueryData(qk.contracts.byId(CONTRACT_ID), readyContract);
    qc.setQueryData(qk.contracts.results(CONTRACT_ID, VERSION_ID), fullResults);
    renderPage(qc);
    await waitFor(() => {
      expect(screen.getByTestId('state-ready')).toBeDefined();
    });
    expect(screen.getByRole('region', { name: 'Ключевые риски' })).toBeDefined();
    expect(screen.getByRole('region', { name: 'Рекомендации' })).toBeDefined();
    expect(screen.getByRole('region', { name: 'Профиль рисков' })).toBeDefined();
    expect(screen.getByRole('region', { name: 'Краткое резюме' })).toBeDefined();
    expect(screen.getByRole('region', { name: 'Отклонения от политики' })).toBeDefined();
    expect(screen.getByTestId('legal-disclaimer')).toBeDefined();
  });

  it('PartiallyFailed — показывает WarningsBanner + полный контент', async () => {
    const qc = makeClient();
    qc.setQueryData(qk.contracts.byId(CONTRACT_ID), partiallyFailedContract);
    qc.setQueryData(qk.contracts.results(CONTRACT_ID, VERSION_ID), fullResults);
    renderPage(qc);
    await waitFor(() => {
      expect(screen.getByTestId('state-ready')).toBeDefined();
    });
    expect(screen.getByTestId('warnings-banner')).toBeDefined();
  });

  it('RBAC Pattern B — BUSINESS_USER видит только Summary, не видит риски', async () => {
    const qc = makeClient();
    qc.setQueryData(qk.contracts.byId(CONTRACT_ID), readyContract);
    qc.setQueryData(qk.contracts.results(CONTRACT_ID, VERSION_ID), businessResults);
    renderPage(qc, businessUser);
    await waitFor(() => {
      expect(screen.getByTestId('state-ready')).toBeDefined();
    });
    expect(screen.getByRole('region', { name: 'Краткое резюме' })).toBeDefined();
    expect(screen.queryByRole('region', { name: 'Ключевые риски' })).toBeNull();
    expect(screen.queryByRole('region', { name: 'Рекомендации' })).toBeNull();
    expect(screen.queryByRole('region', { name: 'Профиль рисков' })).toBeNull();
    expect(screen.queryByRole('region', { name: 'Отклонения от политики' })).toBeNull();
    // BUSINESS_USER без export_enabled — кнопка экспорта скрывается.
    // Тут organization разрешает экспорт, поэтому кнопка видна.
    expect(screen.getByTestId('export-share-button')).toBeDefined();
  });

  it('RiskDetailsDrawer — клик по риску открывает drawer с описанием', async () => {
    const qc = makeClient();
    qc.setQueryData(qk.contracts.byId(CONTRACT_ID), readyContract);
    qc.setQueryData(qk.contracts.results(CONTRACT_ID, VERSION_ID), fullResults);
    renderPage(qc);
    await waitFor(() => {
      expect(screen.getByTestId('state-ready')).toBeDefined();
    });
    const firstRiskButton = screen.getAllByTestId('risks-list-item-button')[0];
    expect(firstRiskButton).toBeDefined();
    fireEvent.click(firstRiskButton!);
    await waitFor(() => {
      expect(screen.getByTestId('risk-details-drawer')).toBeDefined();
    });
    expect(screen.getByTestId('risk-details-description-value').textContent).toContain(
      'Штраф без ограничений',
    );
  });
});
