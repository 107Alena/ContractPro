// Storybook stories ContractDetailPage (FE-TASK-045) — 9 состояний экрана 8
// Figma «Карточка документа» (§17.4).
//
// Декоратор хранит user-сессию и QueryClient: каждая story подставляет своё
// состояние через setQueryData/setQueryDefaults — без MSW, этого достаточно
// для Chromatic visual regression. Полный e2e — Playwright (FE-TASK-055).
import type { Meta, StoryObj } from '@storybook/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ComponentType } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

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

const readyContract = {
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

const analyzingContract = {
  ...readyContract,
  current_version: {
    ...readyContract.current_version,
    processing_status: 'ANALYZING' as const,
    processing_status_message: 'Юридический анализ',
  },
};

const awaitingContract = {
  ...readyContract,
  current_version: {
    ...readyContract.current_version,
    processing_status: 'AWAITING_USER_INPUT' as const,
    processing_status_message: 'Требуется подтверждение типа договора',
  },
};

const failedContract = {
  ...readyContract,
  status: 'ARCHIVED' as const,
  current_version: {
    ...readyContract.current_version,
    processing_status: 'FAILED' as const,
    processing_status_message: 'Ошибка обработки — PDF повреждён',
  },
};

const readyVersions = {
  items: [
    {
      version_id: 'v1',
      version_number: 1,
      processing_status: 'READY' as const,
      origin_type: 'UPLOAD' as const,
      source_file_name: 'alpha-v1.pdf',
      created_at: '2026-04-15T10:00:00Z',
    },
    readyContract.current_version,
  ],
  total: 2,
};

const emptyVersions = { items: [], total: 0 };

interface DecoratorOpts {
  user?: User;
  hydrate?: (qc: QueryClient) => void;
}

function makeDecorator({ user = lawyer, hydrate }: DecoratorOpts) {
  function Decorator(Story: ComponentType): JSX.Element {
    useSession.getState().setUser(user);
    const qc = new QueryClient({
      defaultOptions: {
        queries: { retry: false, retryDelay: 0, gcTime: 0, refetchOnMount: false },
      },
    });
    if (hydrate) hydrate(qc);
    return (
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={[`/contracts/${CONTRACT_ID}`]}>
          <Routes>
            <Route path="/contracts/:id" element={<Story />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    );
  }
  return Decorator;
}

const meta = {
  title: 'Pages/ContractDetailPage',
  component: ContractDetailPage,
  parameters: { layout: 'fullscreen' },
} satisfies Meta<typeof ContractDetailPage>;

export default meta;
type Story = StoryObj<typeof ContractDetailPage>;

/** 1. Default — happy-path: ready contract, 2 версии, LAWYER. */
export const Default: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.byId(CONTRACT_ID), readyContract);
        qc.setQueryData(qk.contracts.versions(CONTRACT_ID), readyVersions);
      },
    }),
  ],
};

/** 2. Loading — contract-query в pending (queryFn навсегда висит). */
export const Loading: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryDefaults(qk.contracts.byId(CONTRACT_ID), {
          queryFn: () => new Promise(() => undefined),
        });
        qc.setQueryDefaults(qk.contracts.versions(CONTRACT_ID), {
          queryFn: () => new Promise(() => undefined),
        });
      },
    }),
  ],
};

/** 3. NotFound — backend 404 CONTRACT_NOT_FOUND. */
export const NotFound: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryDefaults(qk.contracts.byId(CONTRACT_ID), {
          queryFn: () =>
            Promise.reject(
              new OrchestratorError({
                error_code: 'CONTRACT_NOT_FOUND',
                message: 'not found',
                status: 404,
              }),
            ),
        });
      },
    }),
  ],
};

/** 4. ErrorState — сервер 500. */
export const ErrorState: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryDefaults(qk.contracts.byId(CONTRACT_ID), {
          queryFn: () =>
            Promise.reject(
              new OrchestratorError({
                error_code: 'INTERNAL_ERROR',
                message: 'Внутренняя ошибка сервера',
                status: 500,
              }),
            ),
        });
      },
    }),
  ],
};

/** 5. BusinessUser — RBAC скрывает «Ключевые риски» и «Рекомендации». */
export const BusinessUser: Story = {
  decorators: [
    makeDecorator({
      user: businessUser,
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.byId(CONTRACT_ID), readyContract);
        qc.setQueryData(qk.contracts.versions(CONTRACT_ID), readyVersions);
      },
    }),
  ],
};

/** 6. AnalyzingVersion — текущая версия в процессе анализа. */
export const AnalyzingVersion: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.byId(CONTRACT_ID), analyzingContract);
        qc.setQueryData(qk.contracts.versions(CONTRACT_ID), readyVersions);
      },
    }),
  ],
};

/** 7. AwaitingUserInput — требуется подтверждение типа договора. */
export const AwaitingUserInput: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.byId(CONTRACT_ID), awaitingContract);
        qc.setQueryData(qk.contracts.versions(CONTRACT_ID), readyVersions);
      },
    }),
  ],
};

/** 8. Failed — текущая версия в статусе FAILED, договор в архиве. */
export const Failed: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.byId(CONTRACT_ID), failedContract);
        qc.setQueryData(qk.contracts.versions(CONTRACT_ID), {
          items: [failedContract.current_version],
          total: 1,
        });
      },
    }),
  ],
};

/** 9. NoVersions — карточка без версий (edge-case). */
export const NoVersions: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.byId(CONTRACT_ID), {
          ...readyContract,
          current_version: undefined,
        });
        qc.setQueryData(qk.contracts.versions(CONTRACT_ID), emptyVersions);
      },
    }),
  ],
};
