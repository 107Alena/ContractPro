// Storybook stories ContractsListPage (FE-TASK-044) — 9 + 1 tablet состояний
// экрана 7 Figma «Документы» (§17.4).
//
// Декоратор подставляет user-сессию и QueryClient с прехидрированным
// кэшем: каждая story отражает одно из целевых состояний без MSW.
import type { Meta, StoryObj } from '@storybook/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ComponentType } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

import { OrchestratorError, qk } from '@/shared/api';
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

const baseItems = [
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
  {
    contract_id: 'c3',
    title: 'Договор аренды офиса',
    status: 'ACTIVE' as const,
    current_version_number: 1,
    processing_status: 'ANALYZING' as const,
    updated_at: '2026-04-18T09:30:00Z',
  },
];

interface DecoratorOpts {
  user?: User;
  initialEntry?: string;
  hydrate?: (qc: QueryClient) => void;
}

function makeDecorator({
  user = lawyer,
  initialEntry = '/contracts',
  hydrate,
}: DecoratorOpts = {}) {
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
        <MemoryRouter initialEntries={[initialEntry]}>
          <Routes>
            <Route path="/contracts" element={<Story />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    );
  }
  return Decorator;
}

const meta = {
  title: 'Pages/ContractsListPage',
  component: ContractsListPage,
  parameters: { layout: 'fullscreen' },
} satisfies Meta<typeof ContractsListPage>;

export default meta;
type Story = StoryObj<typeof ContractsListPage>;

/** 1. Default — happy path. */
export const Default: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.list({ page: 1, size: 25 }), {
          items: baseItems,
          total: 3,
          page: 1,
          size: 25,
        });
      },
    }),
  ],
};

/** 2. Loading — query в pending. */
export const Loading: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryDefaults(['contracts', 'list'], {
          queryFn: () => new Promise(() => undefined),
        });
      },
    }),
  ],
};

/** 3. Empty (нет договоров) — первое посещение. */
export const Empty: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.list({ page: 1, size: 25 }), {
          items: [],
          total: 0,
          page: 1,
          size: 25,
        });
      },
    }),
  ],
};

/** 4. Empty filtered — ничего не найдено по поиску. */
export const EmptyFiltered: Story = {
  decorators: [
    makeDecorator({
      initialEntry: '/contracts?q=несуществующее',
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.list({ page: 1, size: 25, search: 'несуществующее' }), {
          items: [],
          total: 0,
          page: 1,
          size: 25,
        });
      },
    }),
  ],
};

/** 5. Error — 5xx. */
export const ErrorState: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryDefaults(['contracts', 'list'], {
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

/** 6. Limited Access (BUSINESS_USER) — список виден, колонка действий скрыта. */
export const LimitedAccess: Story = {
  decorators: [
    makeDecorator({
      user: businessUser,
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.list({ page: 1, size: 25 }), {
          items: baseItems,
          total: 3,
          page: 1,
          size: 25,
        });
      },
    }),
  ],
};

/** 7. With Filters — активный фильтр статуса + поиск. */
export const WithFilters: Story = {
  decorators: [
    makeDecorator({
      initialEntry: '/contracts?status=ACTIVE&q=аренда',
      hydrate: (qc) => {
        qc.setQueryData(
          qk.contracts.list({ page: 1, size: 25, status: 'ACTIVE', search: 'аренда' }),
          {
            items: [baseItems[2]!],
            total: 1,
            page: 1,
            size: 25,
          },
        );
      },
    }),
  ],
};

/** 8. Many rows — virtualized DocumentsTable. */
const manyItems = Array.from({ length: 120 }, (_, i) => ({
  contract_id: `bulk-${i}`,
  title: `Договор №${i + 1}`,
  status: 'ACTIVE' as const,
  current_version_number: 1,
  processing_status: 'READY' as const,
  updated_at: '2026-04-19T10:00:00Z',
}));
export const Virtualized: Story = {
  decorators: [
    makeDecorator({
      initialEntry: '/contracts?size=100',
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.list({ page: 1, size: 100 }), {
          items: manyItems,
          total: 120,
          page: 1,
          size: 100,
        });
      },
    }),
  ],
};

/** 9. Paginated — total большой, показаны page controls. */
export const Paginated: Story = {
  decorators: [
    makeDecorator({
      initialEntry: '/contracts?page=2',
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.list({ page: 2, size: 25 }), {
          items: baseItems,
          total: 120,
          page: 2,
          size: 25,
        });
      },
    }),
  ],
};

/** 10. Tablet — layout на tablet-breakpoint (~768px). */
export const Tablet: Story = {
  parameters: { viewport: { defaultViewport: 'tablet' } },
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.list({ page: 1, size: 25 }), {
          items: baseItems,
          total: 3,
          page: 1,
          size: 25,
        });
      },
    }),
  ],
};
