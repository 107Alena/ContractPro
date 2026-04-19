// Smoke-тесты ComparisonPage (FE-TASK-047): покрывают 9 состояний экрана 6.
// Без MSW — feature comparison-start (getDiff/startComparison) замокана; данные
// для success-кейсов подкладываются через QueryClient.setQueryData. lazy
// DiffViewer не материализуется в этих тестах (Suspense fallback виден;
// интерактив diff покрыт unit-тестами widgets/diff-viewer/ui/diff-viewer.test.tsx).
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { OrchestratorError, qk } from '@/shared/api';
import { type User, useSession } from '@/shared/auth';

// Мокаем getDiff/startComparison на уровне их собственных модулей — useDiff
// и useStartComparison импортируют функции через относительные пути, поэтому
// барелл-моки `@/features/comparison-start` НЕ работают.
vi.mock('@/features/comparison-start/api/get-diff', () => ({
  getDiff: vi.fn(),
  getDiffEndpoint: vi.fn(() => '/mock'),
}));
vi.mock('@/features/comparison-start/api/start-comparison', () => ({
  startComparison: vi.fn(),
  startComparisonEndpoint: vi.fn(() => '/mock'),
}));

import { getDiff } from '@/features/comparison-start/api/get-diff';

import { ComparisonPage } from './ComparisonPage';

const lawyer: User = {
  user_id: '00000000-0000-0000-0000-000000000001',
  email: 'maria@example.com',
  name: 'Мария Петрова',
  role: 'LAWYER',
  organization_id: '00000000-0000-0000-0000-000000000099',
  organization_name: 'ООО «Правовой центр»',
  permissions: { export_enabled: true },
};

const businessUser: User = { ...lawyer, role: 'BUSINESS_USER' };

const CONTRACT_ID = 'c1';
const BASE_VID = 'v1';
const TARGET_VID = 'v2';

function makeClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      // retryDelay=0 нужен для error-кейса: useDiff имеет собственный retry-predicate
      // (1 retry для не-DIFF_NOT_FOUND), а retryDelay из defaults не переопределяется.
      // Без этого waitFor таймаутит до завершения exponential-backoff'а.
      queries: { retry: false, retryDelay: 0, gcTime: 0, refetchOnMount: false },
    },
  });
}

function renderAt(url: string, qc: QueryClient, user: User | null = lawyer): void {
  if (user) useSession.getState().setUser(user);
  else useSession.getState().clear();
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[url]}>
        <Routes>
          <Route path="/contracts/:id/compare" element={<ComparisonPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function makeOrchestratorError(code: string, status = 500): OrchestratorError {
  return new OrchestratorError({
    error_code: code,
    message: code === 'DIFF_NOT_FOUND' ? 'Сравнение ещё не готово' : `Ошибка ${code}`,
    status,
    correlationId: 'corr-1',
  });
}

beforeEach(() => {
  vi.mocked(getDiff).mockReset();
});

afterEach(() => {
  cleanup();
  useSession.getState().clear();
});

describe('ComparisonPage — 9 состояний', () => {
  it('1) RoleRestricted: BUSINESS_USER видит inline-сообщение про юристов', () => {
    const qc = makeClient();
    renderAt(
      `/contracts/${CONTRACT_ID}/compare?base=${BASE_VID}&target=${TARGET_VID}`,
      qc,
      businessUser,
    );
    expect(screen.getByTestId('state-role-restricted')).toBeDefined();
    expect(screen.getByText(/Сравнение доступно только юристам/i)).toBeDefined();
  });

  it('2) NoVersionsSelected: ни base, ни target', () => {
    const qc = makeClient();
    renderAt(`/contracts/${CONTRACT_ID}/compare`, qc);
    expect(screen.getByTestId('state-no-versions')).toBeDefined();
  });

  it('3) SingleVersionSelected: только base', () => {
    const qc = makeClient();
    renderAt(`/contracts/${CONTRACT_ID}/compare?base=${BASE_VID}`, qc);
    expect(screen.getByTestId('state-single-version')).toBeDefined();
  });

  it('4) Loading: query в процессе (нет данных и нет ошибки)', () => {
    const qc = makeClient();
    vi.mocked(getDiff).mockImplementation(() => new Promise(() => undefined));
    renderAt(`/contracts/${CONTRACT_ID}/compare?base=${BASE_VID}&target=${TARGET_VID}`, qc);
    expect(screen.getByTestId('state-loading')).toBeDefined();
  });

  it('5) NotReady: 404 DIFF_NOT_FOUND → soft-state', async () => {
    const qc = makeClient();
    vi.mocked(getDiff).mockRejectedValueOnce(makeOrchestratorError('DIFF_NOT_FOUND', 404));
    renderAt(`/contracts/${CONTRACT_ID}/compare?base=${BASE_VID}&target=${TARGET_VID}`, qc);
    await waitFor(() => {
      expect(screen.getByTestId('state-not-ready')).toBeDefined();
    });
  });

  it('6) Error: server 500 → ErrorState с retry', async () => {
    const qc = makeClient();
    vi.mocked(getDiff).mockRejectedValue(makeOrchestratorError('INTERNAL_ERROR', 500));
    renderAt(`/contracts/${CONTRACT_ID}/compare?base=${BASE_VID}&target=${TARGET_VID}`, qc);
    await waitFor(() => {
      expect(screen.getByTestId('state-error')).toBeDefined();
    });
    expect(screen.getByTestId('retry-comparison')).toBeDefined();
  });

  it('7) NoChanges: total=0 → состояние «Изменений нет»', () => {
    const qc = makeClient();
    qc.setQueryData(qk.contracts.diff(CONTRACT_ID, BASE_VID, TARGET_VID), {
      baseVersionId: BASE_VID,
      targetVersionId: TARGET_VID,
      textDiffCount: 0,
      structuralDiffCount: 0,
      textDiffs: [],
      structuralDiffs: [],
    });
    renderAt(`/contracts/${CONTRACT_ID}/compare?base=${BASE_VID}&target=${TARGET_VID}`, qc);
    expect(screen.getByTestId('state-no-changes')).toBeDefined();
  });

  it('8) Ready: рендерит весь набор виджетов сравнения', () => {
    const qc = makeClient();
    qc.setQueryData(qk.contracts.diff(CONTRACT_ID, BASE_VID, TARGET_VID), {
      baseVersionId: BASE_VID,
      targetVersionId: TARGET_VID,
      textDiffCount: 2,
      structuralDiffCount: 1,
      textDiffs: [
        { type: 'added', path: '1/clause-3', old_text: null, new_text: 'Новый пункт.' },
        { type: 'modified', path: '2/clause-1', old_text: 'Старый.', new_text: 'Новый.' },
      ],
      structuralDiffs: [{ type: 'moved', node_id: 'n-1' }],
    });
    renderAt(`/contracts/${CONTRACT_ID}/compare?base=${BASE_VID}&target=${TARGET_VID}`, qc);
    expect(screen.getByTestId('state-ready')).toBeDefined();
    expect(screen.getByTestId('change-counters')).toBeDefined();
    expect(screen.getByTestId('comparison-verdict-card')).toBeDefined();
    expect(screen.getByTestId('risk-profile-delta')).toBeDefined();
    expect(screen.getByTestId('key-diffs-by-section')).toBeDefined();
    expect(screen.getByTestId('changes-table')).toBeDefined();
    expect(screen.getByTestId('tabs-filters')).toBeDefined();
    expect(screen.getByTestId('risks-groups')).toBeDefined();
    // DiffViewer лениво подгружается — ловим Suspense fallback или диффер сам.
    const suspenseOrViewer =
      screen.queryByTestId('diff-viewer-suspense') ?? screen.queryByTestId('diff-viewer-root');
    expect(suspenseOrViewer).not.toBeNull();
  });

  it('9) URL params базируются на ?base=&target= и попадают в заголовок', () => {
    const qc = makeClient();
    qc.setQueryData(qk.contracts.diff(CONTRACT_ID, BASE_VID, TARGET_VID), {
      baseVersionId: BASE_VID,
      targetVersionId: TARGET_VID,
      textDiffCount: 0,
      structuralDiffCount: 0,
      textDiffs: [],
      structuralDiffs: [],
    });
    renderAt(`/contracts/${CONTRACT_ID}/compare?base=${BASE_VID}&target=${TARGET_VID}`, qc);
    expect(
      screen.getByText((content) => content.includes(BASE_VID) && content.includes(TARGET_VID)),
    ).toBeDefined();
  });
});
