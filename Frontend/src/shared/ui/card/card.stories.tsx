import type { Meta, StoryObj } from '@storybook/react';

import { Card } from './card';

const meta: Meta<typeof Card> = {
  title: 'Shared/Card',
  component: Card,
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Card>;

export const Default: Story = {
  args: {
    'aria-label': 'Пример карточки',
    className: 'flex flex-col gap-2 p-5 w-80',
    children: (
      <>
        <p className="text-15 font-semibold text-fg">Заголовок карточки</p>
        <p className="text-13 text-fg-muted">
          Контентная карточка дашборда — radius 12px, тень shadow-sm.
        </p>
      </>
    ),
  },
};

export const Radii: Story = {
  render: () => (
    <div className="flex gap-4">
      <Card
        radius="md"
        aria-label="md"
        className="grid h-24 w-40 place-items-center text-13 text-fg-muted"
      >
        radius=md (10px)
      </Card>
      <Card
        radius="card"
        aria-label="card"
        className="grid h-24 w-40 place-items-center text-13 text-fg-muted"
      >
        radius=card (12px)
      </Card>
      <Card
        radius="xl"
        aria-label="xl"
        className="grid h-24 w-40 place-items-center text-13 text-fg-muted"
      >
        radius=xl (16px)
      </Card>
    </div>
  ),
};
