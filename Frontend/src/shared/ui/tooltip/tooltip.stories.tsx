import type { Meta, StoryObj } from '@storybook/react';

import { Button } from '@/shared/ui/button';

import { SimpleTooltip, Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './tooltip';

const meta = {
  title: 'Shared/Tooltip',
  tags: ['autodocs'],
  parameters: { layout: 'centered' },
  decorators: [
    (Story) => (
      <TooltipProvider delayDuration={200}>
        <div className="p-10">
          <Story />
        </div>
      </TooltipProvider>
    ),
  ],
} satisfies Meta;
export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {
  render: () => (
    <SimpleTooltip content="Откроет карточку договора в новой вкладке">
      <Button variant="secondary">Открыть договор</Button>
    </SimpleTooltip>
  ),
};

export const Sides: Story = {
  render: () => (
    <div className="flex gap-4">
      <SimpleTooltip content="top" side="top">
        <Button variant="secondary">top</Button>
      </SimpleTooltip>
      <SimpleTooltip content="right" side="right">
        <Button variant="secondary">right</Button>
      </SimpleTooltip>
      <SimpleTooltip content="bottom" side="bottom">
        <Button variant="secondary">bottom</Button>
      </SimpleTooltip>
      <SimpleTooltip content="left" side="left">
        <Button variant="secondary">left</Button>
      </SimpleTooltip>
    </div>
  ),
};

export const LongContent: Story = {
  render: () => (
    <SimpleTooltip
      size="md"
      content="Высокий риск: нарушение ст. 451 ГК РФ о существенном изменении обстоятельств. Рекомендуется пересмотреть формулировку."
    >
      <Button variant="ghost">Hover me</Button>
    </SimpleTooltip>
  ),
};

export const DefaultOpen: Story = {
  name: 'defaultOpen (для a11y-snapshot)',
  render: () => (
    <Tooltip defaultOpen>
      <TooltipTrigger asChild>
        <Button variant="secondary">Анкер</Button>
      </TooltipTrigger>
      <TooltipContent>Tooltip открыт сразу</TooltipContent>
    </Tooltip>
  ),
};

export const WithLocalProvider: Story = {
  name: 'withLocalProvider (без глобального Provider)',
  decorators: [(Story) => <Story />],
  render: () => (
    <SimpleTooltip withLocalProvider content="Локальный Provider">
      <Button>Триггер</Button>
    </SimpleTooltip>
  ),
};
