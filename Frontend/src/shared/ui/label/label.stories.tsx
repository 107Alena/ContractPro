import type { Meta, StoryObj } from '@storybook/react';

import { Input } from '../input';
import { Label } from './label';

const meta = {
  title: 'Shared/Label',
  component: Label,
  tags: ['autodocs'],
  args: { children: 'Метка поля' },
} satisfies Meta<typeof Label>;
export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {
  render: (args) => (
    <div style={{ display: 'grid', gap: 'var(--space-2)', maxWidth: 320 }}>
      <Label htmlFor="lbl-default" {...args} />
      <Input id="lbl-default" placeholder="Значение" />
    </div>
  ),
};

export const Required: Story = {
  render: (args) => (
    <div style={{ display: 'grid', gap: 'var(--space-2)', maxWidth: 320 }}>
      <Label htmlFor="lbl-req" required {...args} />
      <Input id="lbl-req" placeholder="Обязательное поле" />
    </div>
  ),
};

export const SizeSmall: Story = {
  render: (args) => (
    <div style={{ display: 'grid', gap: 'var(--space-1)', maxWidth: 320 }}>
      <Label htmlFor="lbl-sm" size="sm" {...args} />
      <Input id="lbl-sm" size="sm" placeholder="sm" />
    </div>
  ),
};
