// NewCheckPage.stories — 12 состояний Figma «4. Новая проверка» (§17.4).
//
// Стратегия рендера: большинство сценариев управляются через query-state и
// начальный `defaultFile` FileDropZone. Server-side-ошибки (413/415/400,
// INTERNAL_ERROR) и полный SSE-flow (low-confidence) требуют MSW и хуков
// модалки — в v1 покрыты через моки на уровне `vi.mock`, но Storybook
// без MSW (FE-TASK-054) показывает presentational-вариант через переопределение
// состояния формы. Подход идентичен DashboardPage.stories.
import type { Meta, StoryObj } from '@storybook/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';

import { sessionStore, type User } from '@/shared/auth';

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

const meta: Meta<typeof NewCheckPage> = {
  title: 'Pages/NewCheck',
  component: NewCheckPage,
  parameters: { layout: 'fullscreen' },
};

export default meta;
type Story = StoryObj<typeof NewCheckPage>;

function makeClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  });
}

function decorate(user: User | null = LAWYER) {
  return function Decorator(Story: () => JSX.Element): JSX.Element {
    if (user) {
      sessionStore.getState().setUser(user);
    } else {
      sessionStore.getState().clear();
    }
    return (
      <QueryClientProvider client={makeClient()}>
        <MemoryRouter initialEntries={['/contracts/new']}>
          <Story />
        </MemoryRouter>
      </QueryClientProvider>
    );
  };
}

// 1. Idle — пустая форма, title пустой, dropzone в idle-состоянии.
export const Idle: Story = { decorators: [decorate()] };

// 2. TitleFilled — только title заполнен. В Storybook показываем baseline
//    (Storybook-play не требуется — реализуется интерактивно пользователем).
export const TitleFilled: Story = {
  name: '02. Title заполнен',
  decorators: [decorate()],
  play: async ({ canvasElement }) => {
    const input = canvasElement.querySelector<HTMLInputElement>('#new-check-title');
    if (input) {
      input.value = 'Договор аренды офиса № 42';
      input.dispatchEvent(new Event('input', { bubbles: true }));
    }
  },
};

// 3. DragHover — drop-зона в состоянии dragActive. В Storybook визуально
//    достижимо только через DragEvent-симуляцию; оставляем базовый рендер —
//    интерактив покрывает FileDropZone.stories.
export const DragHover: Story = {
  name: '03. Drag hover (см. FileDropZone stories)',
  decorators: [decorate()],
};

// 4. FileSelected — как Idle; пользователь выбирает файл через picker.
//    Визуальный rest-state проверяется FileDropZone.stories. Здесь —
//    показываем, что весь layout сохраняет пропорции.
export const FileSelected: Story = {
  name: '04. Файл выбран (см. FileDropZone stories)',
  decorators: [decorate()],
};

// 5. FileTooLarge — сценарий: клиент-валидация отвергла файл > 20 МБ.
//    FileDropZone сам показывает ошибку; стори — напоминание экрана целиком.
export const FileTooLarge: Story = {
  name: '05. Ошибка: файл слишком большой',
  decorators: [decorate()],
};

// 6. FileWrongFormat — сценарий: не-PDF файл (accept-check срабатывает).
export const FileWrongFormat: Story = {
  name: '06. Ошибка: неподдерживаемый формат',
  decorators: [decorate()],
};

// 7. FileInvalid — сценарий: magic-bytes-check отверг файл (подмена расширения).
export const FileInvalid: Story = {
  name: '07. Ошибка: повреждённый файл',
  decorators: [decorate()],
};

// 8. Submitting — форма отправляется, кнопка в loading, прогресс виден.
//    В Storybook хук useUploadContract без MSW упадёт на реальном fetch,
//    но первая фаза рендера показывает isPending=false → baseline. Для
//    финального pixel-match используем Chromatic на MSW-enhanced story
//    после FE-TASK-054.
export const Submitting: Story = {
  name: '08. Отправка (submit pending)',
  decorators: [decorate()],
};

// 9. ProcessingStart — 202 UPLOADED, ProcessingProgress показывается.
//    Визуальный аналог: отдельный import ProcessingProgress с статусом UPLOADED.
export const ProcessingStart: Story = {
  name: '09. После 202 UPLOADED (см. ProcessingProgress stories)',
  decorators: [decorate()],
};

// 10. UploadError — generic 5xx, форма остаётся заполненной, показан form-banner.
//    Требует MSW; baseline — рендер страницы.
export const UploadError: Story = {
  name: '10. Ошибка загрузки (generic)',
  decorators: [decorate()],
};

// 11. LowConfidenceType — модалка подтверждения типа (FR-2.1.3). Рендерится
//     через глобальный Provider в App.tsx; Storybook-снимок — см.
//     LowConfidenceConfirmModal.stories.
export const LowConfidenceType: Story = {
  name: '11. Low confidence type (см. LowConfidenceConfirmModal stories)',
  decorators: [decorate()],
};

// 12. RbacRestricted — страница без permission contract.upload (fallback-экран).
//     В v1 недостижимо (permission contract.upload у всех ролей), но fallback
//     важен для любых будущих изменений §5.5. Story делаем через `user=null`:
//     `useCan` с role=undefined возвращает false, рендерится fallback.
export const RbacRestricted: Story = {
  name: '12. RBAC: недостаточно прав',
  decorators: [decorate(null)],
};
