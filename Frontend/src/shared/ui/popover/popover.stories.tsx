import type { Meta, StoryObj } from '@storybook/react';

import { Button } from '@/shared/ui/button';

import { Popover, PopoverClose, PopoverContent, PopoverTrigger } from './popover';

const meta = {
  title: 'Shared/Popover',
  tags: ['autodocs'],
  parameters: { layout: 'centered' },
  decorators: [
    (Story) => (
      <div className="p-10">
        <Story />
      </div>
    ),
  ],
} satisfies Meta;
export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {
  render: () => (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="secondary">Открыть popover</Button>
      </PopoverTrigger>
      <PopoverContent>
        <p className="mb-2 font-medium">Быстрое действие</p>
        <p className="text-fg-muted">Короткий подсказочный контент с клавой-управлением.</p>
        <div className="mt-3 flex justify-end">
          <PopoverClose asChild>
            <Button size="sm" variant="ghost">
              Закрыть
            </Button>
          </PopoverClose>
        </div>
      </PopoverContent>
    </Popover>
  ),
};

export const SizeSmall: Story = {
  name: 'Size: sm',
  render: () => (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="secondary">sm</Button>
      </PopoverTrigger>
      <PopoverContent size="sm">Компактный popover</PopoverContent>
    </Popover>
  ),
};

export const Sides: Story = {
  render: () => (
    <div className="flex gap-3">
      <Popover>
        <PopoverTrigger asChild>
          <Button variant="secondary">top</Button>
        </PopoverTrigger>
        <PopoverContent side="top">top</PopoverContent>
      </Popover>
      <Popover>
        <PopoverTrigger asChild>
          <Button variant="secondary">right</Button>
        </PopoverTrigger>
        <PopoverContent side="right">right</PopoverContent>
      </Popover>
      <Popover>
        <PopoverTrigger asChild>
          <Button variant="secondary">bottom</Button>
        </PopoverTrigger>
        <PopoverContent side="bottom">bottom</PopoverContent>
      </Popover>
      <Popover>
        <PopoverTrigger asChild>
          <Button variant="secondary">left</Button>
        </PopoverTrigger>
        <PopoverContent side="left">left</PopoverContent>
      </Popover>
    </div>
  ),
};

export const DefaultOpen: Story = {
  name: 'defaultOpen (для a11y-snapshot)',
  render: () => (
    <Popover defaultOpen>
      <PopoverTrigger asChild>
        <Button variant="secondary">Триггер</Button>
      </PopoverTrigger>
      <PopoverContent>Popover открыт сразу для axe</PopoverContent>
    </Popover>
  ),
};
