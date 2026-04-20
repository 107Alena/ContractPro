// Storybook stories ReportsPage (FE-TASK-048) — 10 + 1 tablet состояний экрана
// 9 Figma «Отчёты» (§17.4). Паттерн тот же, что у ContractsListPage.stories:
// прехидрированный QueryClient, без MSW — каждая story отражает одно состояние.
import type { Meta, StoryObj } from '@storybook/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ComponentType } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

import { OrchestratorError, qk } from '@/shared/api';
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
const businessUserNoExport: User = {
  ...lawyer,
  role: 'BUSINESS_USER',
  permissions: { export_enabled: false },
};

const baseItems = [
  {
    contract_id: 'c1',
    title: 'Договор оказания услуг',
    status: 'ACTIVE' as const,
    current_version_number: 2,
    processing_status: 'READY' as const,
    updated_at: '2026-04-18T14:20:00Z',
  },
  {
    contract_id: 'c2',
    title: 'NDA с ООО «Бета»',
    status: 'ACTIVE' as const,
    current_version_number: 1,
    processing_status: 'READY' as const,
    updated_at: '2026-04-15T10:00:00Z',
  },
  {
    contract_id: 'c3',
    title: 'Договор аренды офиса',
    status: 'ACTIVE' as const,
    current_version_number: 1,
    processing_status: 'PARTIALLY_FAILED' as const,
    updated_at: '2026-04-10T09:30:00Z',
  },
];

interface DecoratorOpts {
  user?: User;
  initialEntry?: string;
  hydrate?: (qc: QueryClient) => void;
}

function makeDecorator({ user = lawyer, initialEntry = '/reports', hydrate }: DecoratorOpts = {}) {
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
            <Route path="/reports" element={<Story />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    );
  }
  return Decorator;
}

const meta = {
  title: 'Pages/ReportsPage',
  component: ReportsPage,
  parameters: { layout: 'fullscreen' },
} satisfies Meta<typeof ReportsPage>;

export default meta;
type Story = StoryObj<typeof ReportsPage>;

const DEFAULT_PARAMS = { page: 1, size: 25, status: 'ACTIVE' as const };

/** 1. Default — populated список, фильтр state=READY по умолчанию. */
export const Default: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.list(DEFAULT_PARAMS), {
          items: baseItems,
          total: 24,
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

/** 3. Empty — у пользователя нет готовых отчётов. */
export const Empty: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.list(DEFAULT_PARAMS), {
          items: [],
          total: 0,
          page: 1,
          size: 25,
        });
      },
    }),
  ],
};

/** 4. Filtered Empty — поиск ничего не нашёл. */
export const FilteredEmpty: Story = {
  decorators: [
    makeDecorator({
      initialEntry: '/reports?q=несуществующее',
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.list({ ...DEFAULT_PARAMS, search: 'несуществующее' }), {
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

/** 6. With Detail Panel — выбрана строка, справа ReportDetailPanel. */
export const WithDetailPanel: Story = {
  parameters: { reactRouter: { memoryRouter: { initialEntries: ['/reports'] } } },
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.list(DEFAULT_PARAMS), {
          items: baseItems,
          total: 3,
          page: 1,
          size: 25,
        });
      },
    }),
    (Story) => (
      <div data-preselected-row="c1">
        <Story />
      </div>
    ),
  ],
  play: async ({ canvasElement }) => {
    const row = canvasElement.querySelector('[data-testid="reports-table-row-c1"]');
    if (row instanceof HTMLElement) row.click();
  },
};

/** 7. Expired Link Banner — пользователь пришёл по протухшей ссылке. */
export const ExpiredLink: Story = {
  decorators: [
    makeDecorator({
      initialEntry: '/reports?share=expired',
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.list(DEFAULT_PARAMS), {
          items: baseItems,
          total: 3,
          page: 1,
          size: 25,
        });
      },
    }),
  ],
};

/** 8. Limited Access (BUSINESS_USER без экспорта) — Pattern B: список виден,
 * share-кнопка в detail-panel disabled. */
export const LimitedAccess: Story = {
  decorators: [
    makeDecorator({
      user: businessUserNoExport,
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.list(DEFAULT_PARAMS), {
          items: baseItems,
          total: 3,
          page: 1,
          size: 25,
        });
      },
    }),
    (Story) => (
      <div>
        <Story />
      </div>
    ),
  ],
  play: async ({ canvasElement }) => {
    const row = canvasElement.querySelector('[data-testid="reports-table-row-c1"]');
    if (row instanceof HTMLElement) row.click();
  },
};

/** 9. Paginated — total > page size, PaginationControls видны. */
export const Paginated: Story = {
  decorators: [
    makeDecorator({
      initialEntry: '/reports?page=2',
      hydrate: (qc) => {
        qc.setQueryData({ ...DEFAULT_PARAMS, page: 2 } as never, undefined);
        qc.setQueryData(qk.contracts.list({ ...DEFAULT_PARAMS, page: 2 }), {
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
        qc.setQueryData(qk.contracts.list(DEFAULT_PARAMS), {
          items: baseItems,
          total: 3,
          page: 1,
          size: 25,
        });
      },
    }),
  ],
};
