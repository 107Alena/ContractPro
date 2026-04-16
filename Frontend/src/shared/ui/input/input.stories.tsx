import type { Meta, StoryObj } from '@storybook/react';

import { Label } from '../label';
import { Input } from './input';

const meta = {
  title: 'Shared/Input',
  component: Input,
  tags: ['autodocs'],
  argTypes: {
    type: { control: 'select', options: ['text', 'email', 'password', 'number'] },
    size: { control: 'select', options: ['sm', 'md', 'lg'] },
    error: { control: 'boolean' },
    disabled: { control: 'boolean' },
  },
  args: { placeholder: 'Введите значение' },
} satisfies Meta<typeof Input>;
export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {
  render: (args) => (
    <div style={{ display: 'grid', gap: 'var(--space-2)', maxWidth: 320 }}>
      <Label htmlFor="s-default">Название</Label>
      <Input id="s-default" {...args} />
    </div>
  ),
};

export const Email: Story = {
  args: { type: 'email', placeholder: 'name@example.com' },
  render: (args) => (
    <div style={{ display: 'grid', gap: 'var(--space-2)', maxWidth: 320 }}>
      <Label htmlFor="s-email" required>
        Email
      </Label>
      <Input id="s-email" {...args} />
    </div>
  ),
};

export const Password: Story = {
  args: { type: 'password', placeholder: '••••••••' },
  render: (args) => (
    <div style={{ display: 'grid', gap: 'var(--space-2)', maxWidth: 320 }}>
      <Label htmlFor="s-password">Пароль</Label>
      <Input id="s-password" {...args} />
    </div>
  ),
};

export const Number_: Story = {
  name: 'Number',
  args: { type: 'number', placeholder: '0' },
};

export const ErrorState: Story = {
  name: 'Error',
  args: { error: true, placeholder: 'Неверный формат' },
  render: (args) => (
    <div style={{ display: 'grid', gap: 'var(--space-2)', maxWidth: 320 }}>
      <Label htmlFor="s-error">Email</Label>
      <Input id="s-error" aria-describedby="s-error-hint" defaultValue="not-an-email" {...args} />
      <p id="s-error-hint" style={{ color: 'var(--color-danger)', fontSize: 12, margin: 0 }}>
        Укажите корректный email
      </p>
    </div>
  ),
};

export const Disabled: Story = {
  args: { disabled: true, defaultValue: 'readonly value' },
};

export const Sizes: Story = {
  render: () => (
    <div style={{ display: 'grid', gap: 'var(--space-3)', maxWidth: 320 }}>
      <Input placeholder="sm" size="sm" />
      <Input placeholder="md" size="md" />
      <Input placeholder="lg" size="lg" />
    </div>
  ),
};
