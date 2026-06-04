// DashboardPage.stories — 4 состояния из AC FE-TASK-042 (Default/Loading/Empty/Error).
// Используем setQueryData для подстановки данных без MSW (FE-TASK-054).
// useEventStream не мокается — в Storybook нет EventSource, но хук
// безопасно возвращается через noop при отсутствии globalThis.EventSource.
import type { Meta, StoryObj } from '@storybook/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import type { ContractSummary } from '@/entities/contract';
import type { UserProfile } from '@/entities/user';
import { qk } from '@/shared/api';
import { sessionStore } from '@/shared/auth';

import { DashboardPage } from './DashboardPage';

const meta: Meta<typeof DashboardPage> = {
  title: 'Pages/Dashboard',
  component: DashboardPage,
  parameters: { layout: 'fullscreen' },
};

export default meta;

const user: UserProfile = {
  user_id: '00000000-0000-0000-0000-000000000001',
  email: 'maria@company.ru',
  name: 'Мария Петрова',
  role: 'LAWYER',
  organization_id: '00000000-0000-0000-0000-000000000002',
  organization_name: 'ООО «Правовой центр»',
  permissions: { export_enabled: true },
};

const items: ContractSummary[] = [
  {
    contract_id: 'c1',
    title: 'Договор аренды офиса',
    processing_status: 'READY',
    current_version_number: 2,
    updated_at: '2026-04-15T09:30:00Z',
  },
  {
    contract_id: 'c2',
    title: 'Услуги консалтинга',
    processing_status: 'ANALYZING',
    updated_at: '2026-04-17T10:05:00Z',
  },
  {
    contract_id: 'c3',
    title: 'NDA с подрядчиком',
    processing_status: 'AWAITING_USER_INPUT',
    updated_at: '2026-04-17T11:15:00Z',
  },
];

type SeedOptions = {
  me?: UserProfile;
  contracts?: { items: ContractSummary[]; total: number };
  contractsError?: boolean;
  stats?: { total: number; inProgress: number };
};

function seed({ me, contracts, contractsError, stats }: SeedOptions): QueryClient {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  if (me) qc.setQueryData(qk.me, me);
  if (contracts) qc.setQueryData(qk.contracts.list({ size: 5 }), contracts);
  if (stats) {
    qc.setQueryData(qk.contracts.stats, {
      total: stats.total,
      by_processing_status: {
        uploaded: 0,
        queued: 0,
        processing: 0,
        // analyzing несёт весь «в работе» — упрощение для сторибука
        analyzing: stats.inProgress,
        awaiting_user_input: 0,
        generating_reports: 0,
        ready: 0,
        partially_failed: 0,
        failed: 0,
        rejected: 0,
        not_started: 0,
      },
    });
  }
  if (contractsError) {
    qc.getQueryCache()
      .build(qc, {
        queryKey: qk.contracts.list({ size: 5 }),
        queryFn: () => Promise.reject(new Error('storybook-error')),
      })
      .setState({
        data: undefined,
        status: 'error',
        error: new Error('Сеть недоступна'),
        dataUpdatedAt: 0,
        errorUpdatedAt: Date.now(),
        fetchStatus: 'idle',
        fetchFailureCount: 1,
        fetchFailureReason: new Error('Сеть недоступна'),
      });
  }
  if (me) {
    sessionStore.getState().setUser(me);
  } else {
    sessionStore.getState().clear();
  }
  return qc;
}

type Story = StoryObj<typeof DashboardPage>;

function decorate(qc: QueryClient) {
  return function Decorator(Story: () => JSX.Element): JSX.Element {
    return (
      <QueryClientProvider client={qc}>
        <Story />
      </QueryClientProvider>
    );
  };
}

export const Default: Story = {
  decorators: [
    decorate(
      seed({ me: user, contracts: { items, total: 12 }, stats: { total: 12, inProgress: 3 } }),
    ),
  ],
};

export const Loading: Story = {
  // без setQueryData TanStack Query стартует fetch; в Storybook без MSW
  // запрос упадёт, но первая фаза render'а — `isLoading` на всех виджетах.
  decorators: [decorate(seed({}))],
};

export const Empty: Story = {
  decorators: [
    decorate(
      seed({ me: user, contracts: { items: [], total: 0 }, stats: { total: 0, inProgress: 0 } }),
    ),
  ],
};

export const ErrorState: Story = {
  // Список в ошибке; stats сидим нулями, чтобы не дёргать сеть в Storybook.
  decorators: [
    decorate(seed({ me: user, contractsError: true, stats: { total: 0, inProgress: 0 } })),
  ],
};
