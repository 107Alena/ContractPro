// @vitest-environment jsdom
//
// Smoke-тесты NewCheckPage (FE-TASK-043): проверяют базовые состояния из AC
// через rendering + mocked useUploadContract. SSE/модалка low-confidence
// глобальны — тест инстанцирует page без Provider'а; интеграция покрыта в
// features/low-confidence-confirm tests.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { sessionStore, type User } from '@/shared/auth';

// Мокаем feature-модуль до импорта NewCheckPage, чтобы контролировать
// upload-pipeline без реальных HTTP-запросов. Сохраняем UPLOAD_FORM_FIELDS
// и типы, подменяем только useUploadContract.
const uploadSpy = vi.fn();
const resetSpy = vi.fn();

vi.mock('@/features/contract-upload', async (importActual) => {
  const actual = await importActual<typeof import('@/features/contract-upload')>();
  return {
    ...actual,
    useUploadContract: (opts: unknown) => {
      // Сохраняем опции, чтобы тест мог вызвать onSuccess/onError вручную,
      // эмулируя server-response без MSW.
      lastOptions = opts as Record<string, unknown>;
      return {
        upload: uploadSpy,
        uploadAsync: vi.fn(),
        cancel: vi.fn(),
        isPending: false,
        isSuccess: false,
        isError: false,
        reset: resetSpy,
        data: undefined,
        error: null,
        status: 'idle',
      };
    },
  };
});

let lastOptions: Record<string, unknown> = {};

// navigate — spy, чтобы убедиться в редиректе на ResultPage.
const navigateSpy = vi.fn();

vi.mock('react-router-dom', async (importActual) => {
  const actual = await importActual<typeof import('react-router-dom')>();
  return {
    ...actual,
    useNavigate: () => navigateSpy,
  };
});

import { NewCheckPage } from './NewCheckPage';

const LAWYER: User = {
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
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
}

function renderPage(user: User | null = LAWYER) {
  if (user) sessionStore.getState().setUser(user);
  else sessionStore.getState().clear();
  return render(
    <QueryClientProvider client={makeClient()}>
      <MemoryRouter>
        <NewCheckPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  sessionStore.getState().clear();
  uploadSpy.mockReset();
  resetSpy.mockReset();
  navigateSpy.mockReset();
  lastOptions = {};
});

describe('NewCheckPage', () => {
  it('рендерит главный заголовок «Новая проверка»', () => {
    renderPage();
    expect(
      screen.getByRole('heading', { level: 1, name: /новая проверка/i }),
    ).toBeDefined();
  });

  it('показывает форму: title input + file-dropzone + submit', () => {
    renderPage();
    expect(screen.getByLabelText(/название договора/i)).toBeDefined();
    expect(screen.getByLabelText(/файл договора/i)).toBeDefined();
    expect(screen.getByRole('button', { name: /начать проверку/i })).toBeDefined();
  });

  it('submit disabled пока title и file не заполнены', () => {
    renderPage();
    const submit = screen.getByRole('button', { name: /начать проверку/i });
    expect(submit.hasAttribute('disabled')).toBe(true);
  });

  it('валидация при submit: если title пустой, показывается ошибка', () => {
    renderPage();
    // Используем data-testid вместо role=form — implicit form-role требует
    // aria-labelledby по ARIA 1.2; надёжнее через testid.
    const form = screen.getByTestId('new-check-form');
    fireEvent.submit(form);
    expect(screen.getByText(/укажите название договора/i)).toBeDefined();
  });

  it('табы: переключение на «Вставить текст» показывает placeholder', () => {
    renderPage();
    const pasteTab = screen.getByRole('tab', { name: /вставить текст/i });
    fireEvent.click(pasteTab);
    expect(screen.getByText(/вставка текста появится позже/i)).toBeDefined();
  });

  it('виджеты WillHappenSteps и WhatWeCheck видны', () => {
    renderPage();
    expect(screen.getByRole('region', { name: 'Что произойдёт' })).toBeDefined();
    expect(screen.getByRole('region', { name: 'Что мы проверяем' })).toBeDefined();
  });

  it('RBAC: user=null → fallback «Недостаточно прав»', () => {
    renderPage(null);
    expect(
      screen.getByRole('heading', { level: 1, name: /недостаточно прав/i }),
    ).toBeDefined();
  });

  it('onSuccess → navigate(/contracts/:id/versions/:vid/result)', () => {
    renderPage();
    const onSuccess = lastOptions.onSuccess as
      | ((d: {
          contractId: string;
          versionId: string;
          versionNumber: number;
          jobId: string;
          status: 'UPLOADED';
        }) => void)
      | undefined;
    expect(onSuccess).toBeTypeOf('function');
    onSuccess?.({
      contractId: 'c1',
      versionId: 'v1',
      versionNumber: 1,
      jobId: 'j1',
      status: 'UPLOADED',
    });
    expect(navigateSpy).toHaveBeenCalledWith('/contracts/c1/versions/v1/result');
  });
});
