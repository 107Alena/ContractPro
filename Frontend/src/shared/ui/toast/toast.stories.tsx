import type { Meta, StoryObj } from '@storybook/react';
import { expect, userEvent, within } from '@storybook/test';
import { useEffect } from 'react';

import { Button } from '@/shared/ui/button';

import { __resetToastStoreForTests } from './toast-store';
import { Toaster } from './toaster';
import { toast } from './use-toast';

const meta = {
  title: 'Shared/Toast',
  tags: ['autodocs'],
  decorators: [
    (Story) => (
      <div className="flex min-h-[260px] flex-col items-start gap-3 p-4">
        <Story />
        <Toaster />
      </div>
    ),
  ],
} satisfies Meta;
export default meta;

type Story = StoryObj<typeof meta>;

function ResetOnMount() {
  useEffect(() => {
    __resetToastStoreForTests();
  }, []);
  return null;
}

export const Variants: Story = {
  render: () => (
    <>
      <ResetOnMount />
      <div className="flex flex-wrap gap-2">
        <Button onClick={() => toast.success('Договор сохранён')}>success</Button>
        <Button variant="danger" onClick={() => toast.error('Не удалось сохранить')}>
          error
        </Button>
        <Button variant="secondary" onClick={() => toast.warn('Низкая уверенность классификатора')}>
          warning
        </Button>
        <Button variant="ghost" onClick={() => toast.info('Проверка запущена')}>
          info
        </Button>
        <Button
          onClick={() =>
            toast.sticky({
              title: 'Нет соединения',
              description: 'Повторная попытка автоматически.',
            })
          }
        >
          sticky
        </Button>
      </div>
    </>
  ),
};

export const WithAction: Story = {
  render: () => (
    <>
      <ResetOnMount />
      <Button
        onClick={() =>
          toast.error({
            title: 'Ошибка загрузки',
            description: 'Попробуйте ещё раз.',
            action: { label: 'Повторить', onClick: (id) => toast.dismiss(id) },
          })
        }
      >
        Показать toast с action
      </Button>
    </>
  ),
};

export const FifoLimit: Story = {
  name: 'FIFO лимит 5',
  render: () => (
    <>
      <ResetOnMount />
      <Button
        onClick={() => {
          for (let i = 1; i <= 7; i += 1) {
            toast.info(`Toast #${i}`);
          }
        }}
      >
        Показать 7 подряд
      </Button>
    </>
  ),
  play: async ({ canvasElement, step }) => {
    const canvas = within(canvasElement);
    const button = canvas.getByRole('button', { name: 'Показать 7 подряд' });
    await step('Кликаем — показываем 7 тостов', async () => {
      await userEvent.click(button);
    });
    await step('На экране не больше 5 status/alert live-regions', async () => {
      const live = document.querySelectorAll('[role="status"], [role="alert"]');
      await expect(live.length).toBeLessThanOrEqual(5);
    });
  },
};
