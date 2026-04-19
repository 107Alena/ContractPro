// Storybook stories ResultPage (FE-TASK-046) — 8 состояний экрана 5 Figma
// «Результат» (§17.4). Декоратор подставляет user-сессию и QueryClient;
// каждая story через setQueryData/setQueryDefaults имитирует backend-ответ.
// MSW не используется — достаточно для Chromatic visual regression.
import type { Meta, StoryObj } from '@storybook/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ComponentType } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

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
const processingContract = makeContract('PROCESSING', 'Юридический анализ');
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
    penalties: 'Ограниченная ответственность',
    jurisdiction: 'Москва',
  },
  risk_profile: { overall_level: 'medium' as const, high_count: 1, medium_count: 2, low_count: 4 },
  risks: [
    {
      id: 'r1',
      level: 'high' as const,
      description: 'Штраф 10% от суммы без ограничений по сроку.',
      clause_ref: '5.3',
      legal_basis: 'ГК РФ ст. 330',
    },
    {
      id: 'r2',
      level: 'medium' as const,
      description: 'Срок оплаты не согласован с внутренней политикой.',
      clause_ref: '3.1',
      legal_basis: 'Внутренняя политика организации',
    },
    {
      id: 'r3',
      level: 'low' as const,
      description: 'Неточность в формулировке предмета договора.',
      clause_ref: '1.2',
    },
  ],
  recommendations: [
    {
      risk_id: 'r1',
      original_text: 'штраф 10% от суммы',
      recommended_text: 'штраф не более 0,1% за каждый день просрочки, общая сумма — не более 10%',
      explanation: 'Ограничивает максимальный размер штрафа, защищая обе стороны.',
    },
  ],
  summary:
    'Договор оказания консалтинговых услуг на 12 месяцев за 1 200 000 ₽. Требуется согласовать штрафные санкции и срок оплаты.',
  aggregate_score: { score: 0.62, label: 'Средний риск' },
};

const businessResults = {
  version_id: VERSION_ID,
  status: 'READY' as const,
  key_parameters: fullResults.key_parameters,
  summary: fullResults.summary,
  aggregate_score: fullResults.aggregate_score,
};

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
        <MemoryRouter initialEntries={[`/contracts/${CONTRACT_ID}/versions/${VERSION_ID}/result`]}>
          <Routes>
            <Route path="/contracts/:id/versions/:vid/result" element={<Story />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    );
  }
  return Decorator;
}

const meta = {
  title: 'Pages/ResultPage',
  component: ResultPage,
  parameters: { layout: 'fullscreen' },
} satisfies Meta<typeof ResultPage>;

export default meta;
type Story = StoryObj<typeof ResultPage>;

/** 1. Ready_Lawyer — happy-path: LAWYER видит все секции. */
export const Ready_Lawyer: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.byId(CONTRACT_ID), readyContract);
        qc.setQueryData(qk.contracts.results(CONTRACT_ID, VERSION_ID), fullResults);
      },
    }),
  ],
};

/** 2. Ready_BusinessUser — RBAC скрыты risks/recommendations/profile/deviations. */
export const Ready_BusinessUser: Story = {
  decorators: [
    makeDecorator({
      user: businessUser,
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.byId(CONTRACT_ID), readyContract);
        qc.setQueryData(qk.contracts.results(CONTRACT_ID, VERSION_ID), businessResults);
      },
    }),
  ],
};

/** 3. Processing — inline banner + ProcessingProgress. */
export const Processing: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.byId(CONTRACT_ID), processingContract);
      },
    }),
  ],
};

/** 4. ReadyWithWarnings — READY-подобный контент + warnings banner. */
export const ReadyWithWarnings: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.byId(CONTRACT_ID), partiallyFailedContract);
        qc.setQueryData(qk.contracts.results(CONTRACT_ID, VERSION_ID), fullResults);
      },
    }),
  ],
};

/** 5. AwaitingUserInput — CTA для подтверждения типа договора. */
export const AwaitingUserInput: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.byId(CONTRACT_ID), awaitingContract);
      },
    }),
  ],
};

/** 6. Failed — ошибка анализа + RecheckButton. */
export const Failed: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.byId(CONTRACT_ID), failedContract);
      },
    }),
  ],
};

/** 7. Rejected — файл отклонён, CTA «Заменить файл». */
export const Rejected: Story = {
  decorators: [
    makeDecorator({
      hydrate: (qc) => {
        qc.setQueryData(qk.contracts.byId(CONTRACT_ID), rejectedContract);
      },
    }),
  ],
};

/** 8. NotFound — версия/договор удалён (CONTRACT_NOT_FOUND). */
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
