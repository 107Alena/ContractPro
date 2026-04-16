import type { Meta, StoryObj } from '@storybook/react';
import { expect, userEvent, within } from '@storybook/test';

import { Button } from './button';

const meta = {
  title: 'Shared/Button',
  component: Button,
  tags: ['autodocs'],
  argTypes: {
    variant: { control: 'select', options: ['primary', 'secondary', 'ghost', 'danger'] },
    size: { control: 'select', options: ['sm', 'md', 'lg'] },
    fullWidth: { control: 'boolean' },
    loading: { control: 'boolean' },
    disabled: { control: 'boolean' },
  },
  args: { children: 'Кнопка' },
} satisfies Meta<typeof Button>;
export default meta;

type Story = StoryObj<typeof meta>;

export const Primary: Story = { args: { variant: 'primary' } };
export const Secondary: Story = { args: { variant: 'secondary' } };
export const Ghost: Story = { args: { variant: 'ghost' } };
export const Danger: Story = { args: { variant: 'danger' } };

export const SizeSmall: Story = { args: { size: 'sm', children: 'Small' } };
export const SizeMedium: Story = { args: { size: 'md', children: 'Medium' } };
export const SizeLarge: Story = { args: { size: 'lg', children: 'Large' } };

export const Disabled: Story = { args: { disabled: true } };
export const Loading: Story = { args: { loading: true, children: 'Загрузка' } };

const IconLeft = () => (
  <svg aria-hidden="true" width="16" height="16" viewBox="0 0 16 16" fill="none">
    <path d="M8 2v12M2 8h12" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" />
  </svg>
);

export const WithIconLeft: Story = {
  args: { iconLeft: <IconLeft />, children: 'Добавить' },
};

export const WithIconRight: Story = {
  args: { iconRight: <IconLeft />, children: 'Далее' },
};

export const FullWidth: Story = {
  args: { fullWidth: true, children: 'Во всю ширину' },
  parameters: { layout: 'padded' },
};

export const AsChildLink: Story = {
  args: {
    asChild: true,
    variant: 'secondary',
    children: <a href="#section">Ссылка-кнопка</a>,
  },
};

export const AsChildLinkDisabled: Story = {
  name: 'AsChild + disabled',
  args: {
    asChild: true,
    disabled: true,
    variant: 'secondary',
    children: <a href="#section">Ссылка-кнопка (disabled)</a>,
  },
};

export const KeyboardActivation: Story = {
  name: 'A11y: keyboard activation',
  args: { variant: 'primary', children: 'Активация с клавиатуры' },
  play: async ({ canvasElement, step }) => {
    const canvas = within(canvasElement);
    const button = canvas.getByRole('button', { name: 'Активация с клавиатуры' });

    await step('Tab фокусирует кнопку', async () => {
      await userEvent.tab();
      await expect(button).toHaveFocus();
    });

    await step('Enter активирует кнопку', async () => {
      let clicked = 0;
      button.addEventListener('click', () => (clicked += 1), { once: true });
      await userEvent.keyboard('{Enter}');
      await expect(clicked).toBe(1);
    });

    await step('Space активирует кнопку', async () => {
      let clicked = 0;
      button.addEventListener('click', () => (clicked += 1), { once: true });
      await userEvent.keyboard(' ');
      await expect(clicked).toBe(1);
    });
  },
};
