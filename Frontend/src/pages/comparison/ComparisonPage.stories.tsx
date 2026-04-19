// Storybook stories ComparisonPage (FE-TASK-047) — 9 состояний экрана 6 Figma.
//
// Каждая story монтирует страницу с уникальной комбинацией QueryClient-состояния
// и user-роли через decorator. Без MSW: данные кладутся в кэш или подкладывается
// reject-фейк через setQueryDefaults — этого достаточно для визуальной regression
// в Chromatic. Полное e2e — Playwright (FE-TASK-055).
import type { Meta, StoryObj } from '@storybook/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ComponentType } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

import { OrchestratorError, qk } from '@/shared/api';
import { type User, useSession } from '@/shared/auth';

import { ComparisonPage } from './ComparisonPage';

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

const CONTRACT_ID = 'c1';
const BASE_VID = 'v1';
const TARGET_VID = 'v2';

interface DecoratorOpts {
  url: string;
  user?: User;
  hydrate?: (qc: QueryClient) => void;
}

function makeDecorator({ url, user = lawyer, hydrate }: DecoratorOpts) {
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
        <MemoryRouter initialEntries={[url]}>
          <Routes>
            <Route path="/contracts/:id/compare" element={<Story />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    );
  }
  return Decorator;
}

const sampleDiff = {
  baseVersionId: BASE_VID,
  targetVersionId: TARGET_VID,
  textDiffCount: 6,
  structuralDiffCount: 2,
  textDiffs: [
    {
      type: 'added',
      path: '1/clause-3',
      old_text: null,
      new_text: 'Срок поставки уменьшен до 14 дней.',
    },
    {
      type: 'modified',
      path: '2/clause-1',
      old_text: 'Цена 500 000 ₽.',
      new_text: 'Цена 480 000 ₽ с НДС.',
    },
    {
      type: 'modified',
      path: '2/clause-2',
      old_text: 'Авансовый платёж 50%.',
      new_text: 'Авансовый платёж 30%.',
    },
    {
      type: 'removed',
      path: '3/clause-7',
      old_text: 'Штраф за просрочку — 0.5%/день.',
      new_text: null,
    },
    { type: 'added', path: '4/clause-1', old_text: null, new_text: 'Гарантия 12 месяцев.' },
    {
      type: 'modified',
      path: '5/clause-4',
      old_text: 'Подсудность по месту истца.',
      new_text: 'Подсудность по соглашению сторон.',
    },
  ],
  structuralDiffs: [
    { type: 'moved', node_id: 'section-3' },
    { type: 'added', node_id: 'section-7' },
  ],
};

const meta = {
  title: 'Pages/ComparisonPage',
  component: ComparisonPage,
  parameters: { layout: 'fullscreen' },
} satisfies Meta<typeof ComparisonPage>;

export default meta;
type Story = StoryObj<typeof ComparisonPage>;

/** 1. Базовое состояние с обеими версиями и заполненным diff'ом — основной happy-path. */
export const Ready: Story = {
  decorators: [
    makeDecorator({
      url: `/contracts/${CONTRACT_ID}/compare?base=${BASE_VID}&target=${TARGET_VID}`,
      hydrate: (qc) =>
        qc.setQueryData(qk.contracts.diff(CONTRACT_ID, BASE_VID, TARGET_VID), sampleDiff),
    }),
  ],
};

/** 2. Loading — diff ещё не пришёл (query в pending; queryFn навсегда висит). */
export const Loading: Story = {
  decorators: [
    makeDecorator({
      url: `/contracts/${CONTRACT_ID}/compare?base=${BASE_VID}&target=${TARGET_VID}`,
      hydrate: (qc) =>
        qc.setQueryDefaults(qk.contracts.diff(CONTRACT_ID, BASE_VID, TARGET_VID), {
          queryFn: () => new Promise(() => undefined),
        }),
    }),
  ],
};

/** 3. NotReady — backend вернул 404 DIFF_NOT_FOUND. */
export const NotReady: Story = {
  decorators: [
    makeDecorator({
      url: `/contracts/${CONTRACT_ID}/compare?base=${BASE_VID}&target=${TARGET_VID}`,
      hydrate: (qc) =>
        qc.setQueryDefaults(qk.contracts.diff(CONTRACT_ID, BASE_VID, TARGET_VID), {
          queryFn: () =>
            Promise.reject(
              new OrchestratorError({
                error_code: 'DIFF_NOT_FOUND',
                message: 'Сравнение ещё не готово',
                status: 404,
              }),
            ),
        }),
    }),
  ],
};

/** 4. ErrorState — server 500. */
export const ErrorState: Story = {
  decorators: [
    makeDecorator({
      url: `/contracts/${CONTRACT_ID}/compare?base=${BASE_VID}&target=${TARGET_VID}`,
      hydrate: (qc) =>
        qc.setQueryDefaults(qk.contracts.diff(CONTRACT_ID, BASE_VID, TARGET_VID), {
          queryFn: () =>
            Promise.reject(
              new OrchestratorError({
                error_code: 'INTERNAL_ERROR',
                message: 'Внутренняя ошибка сервера',
                status: 500,
              }),
            ),
        }),
    }),
  ],
};

/** 5. NoVersionsSelected — URL без параметров. */
export const NoVersionsSelected: Story = {
  decorators: [makeDecorator({ url: `/contracts/${CONTRACT_ID}/compare` })],
};

/** 6. SingleVersionSelected — выбрана только одна версия. */
export const SingleVersionSelected: Story = {
  decorators: [makeDecorator({ url: `/contracts/${CONTRACT_ID}/compare?base=${BASE_VID}` })],
};

/** 7. NoChanges — diff пришёл, но изменений нет. */
export const NoChanges: Story = {
  decorators: [
    makeDecorator({
      url: `/contracts/${CONTRACT_ID}/compare?base=${BASE_VID}&target=${TARGET_VID}`,
      hydrate: (qc) =>
        qc.setQueryData(qk.contracts.diff(CONTRACT_ID, BASE_VID, TARGET_VID), {
          baseVersionId: BASE_VID,
          targetVersionId: TARGET_VID,
          textDiffCount: 0,
          structuralDiffCount: 0,
          textDiffs: [],
          structuralDiffs: [],
        }),
    }),
  ],
};

/** 8. RoleRestricted — BUSINESS_USER не имеет доступа к comparison.run. */
export const RoleRestricted: Story = {
  decorators: [
    makeDecorator({
      url: `/contracts/${CONTRACT_ID}/compare?base=${BASE_VID}&target=${TARGET_VID}`,
      user: businessUser,
    }),
  ],
};

/** 9. ManyChanges — крупный diff (демонстрирует ChangesTable + RisksGroups + lazy DiffViewer). */
export const ManyChanges: Story = {
  decorators: [
    makeDecorator({
      url: `/contracts/${CONTRACT_ID}/compare?base=${BASE_VID}&target=${TARGET_VID}`,
      hydrate: (qc) => {
        const big = {
          baseVersionId: BASE_VID,
          targetVersionId: TARGET_VID,
          textDiffCount: 24,
          structuralDiffCount: 8,
          textDiffs: Array.from({ length: 24 }, (_, i) => ({
            type: i % 3 === 0 ? 'added' : i % 3 === 1 ? 'removed' : 'modified',
            path: `${(i % 5) + 1}/clause-${(i % 12) + 1}`,
            old_text: i % 3 === 0 ? null : `Старая редакция пункта ${i + 1}.`,
            new_text: i % 3 === 1 ? null : `Новая редакция пункта ${i + 1}.`,
          })),
          structuralDiffs: Array.from({ length: 8 }, (_, i) => ({
            type: ['added', 'removed', 'modified', 'moved'][i % 4],
            node_id: `section-${i}`,
          })),
        };
        qc.setQueryData(qk.contracts.diff(CONTRACT_ID, BASE_VID, TARGET_VID), big);
      },
    }),
  ],
};
