import type { Meta, StoryObj } from '@storybook/react';

import { Badge } from './badge';

const meta = {
  title: 'Shared/Badge',
  component: Badge,
  tags: ['autodocs'],
  argTypes: {
    variant: {
      control: 'select',
      options: ['success', 'warning', 'danger', 'neutral', 'brand'],
    },
  },
  args: { children: 'Badge' },
} satisfies Meta<typeof Badge>;
export default meta;

type Story = StoryObj<typeof meta>;

export const Success: Story = { args: { variant: 'success', children: 'Готово' } };
export const Warning: Story = { args: { variant: 'warning', children: 'Требует внимания' } };
export const Danger: Story = { args: { variant: 'danger', children: 'Высокий риск' } };
export const Neutral: Story = { args: { variant: 'neutral', children: 'Черновик' } };
export const Brand: Story = { args: { variant: 'brand', children: 'Новое' } };

export const AllVariants: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
      <Badge variant="success">Готово</Badge>
      <Badge variant="warning">Требует внимания</Badge>
      <Badge variant="danger">Высокий риск</Badge>
      <Badge variant="neutral">Черновик</Badge>
      <Badge variant="brand">Новое</Badge>
    </div>
  ),
};
