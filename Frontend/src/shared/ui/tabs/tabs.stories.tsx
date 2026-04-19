import type { Meta, StoryObj } from '@storybook/react';

import { Tabs, TabsContent, TabsList, TabsTrigger } from './tabs';

const meta = {
  title: 'Shared/Tabs',
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
} satisfies Meta;
export default meta;

type Story = StoryObj<typeof meta>;

const Panel = ({ children }: { children: string }) => (
  <div className="rounded-md border border-border bg-bg p-4 text-sm text-fg">{children}</div>
);

export const Default: Story = {
  render: () => (
    <Tabs defaultValue="overview">
      <TabsList aria-label="Разделы договора">
        <TabsTrigger value="overview">Обзор</TabsTrigger>
        <TabsTrigger value="risks">Риски</TabsTrigger>
        <TabsTrigger value="history">История</TabsTrigger>
      </TabsList>
      <TabsContent value="overview">
        <Panel>Обзор договора: тип, стороны, ключевые параметры.</Panel>
      </TabsContent>
      <TabsContent value="risks">
        <Panel>Список выявленных рисков и рекомендаций.</Panel>
      </TabsContent>
      <TabsContent value="history">
        <Panel>История версий документа.</Panel>
      </TabsContent>
    </Tabs>
  ),
};

export const Pills: Story = {
  render: () => (
    <Tabs defaultValue="all">
      <TabsList variant="pills" aria-label="Фильтр по статусу">
        <TabsTrigger variant="pills" value="all">
          Все
        </TabsTrigger>
        <TabsTrigger variant="pills" value="active">
          Активные
        </TabsTrigger>
        <TabsTrigger variant="pills" value="archived">
          Архив
        </TabsTrigger>
      </TabsList>
      <TabsContent value="all">
        <Panel>Все документы.</Panel>
      </TabsContent>
      <TabsContent value="active">
        <Panel>Только активные.</Panel>
      </TabsContent>
      <TabsContent value="archived">
        <Panel>Архивные документы.</Panel>
      </TabsContent>
    </Tabs>
  ),
};

export const FullWidth: Story = {
  render: () => (
    <Tabs defaultValue="a">
      <TabsList variant="pills" fullWidth aria-label="Full width tabs">
        <TabsTrigger variant="pills" fullWidth value="a">
          Раздел A
        </TabsTrigger>
        <TabsTrigger variant="pills" fullWidth value="b">
          Раздел B
        </TabsTrigger>
        <TabsTrigger variant="pills" fullWidth value="c">
          Раздел C
        </TabsTrigger>
      </TabsList>
      <TabsContent value="a">
        <Panel>A</Panel>
      </TabsContent>
      <TabsContent value="b">
        <Panel>B</Panel>
      </TabsContent>
      <TabsContent value="c">
        <Panel>C</Panel>
      </TabsContent>
    </Tabs>
  ),
};

export const WithDisabled: Story = {
  render: () => (
    <Tabs defaultValue="a">
      <TabsList aria-label="Tabs with disabled">
        <TabsTrigger value="a">Доступно</TabsTrigger>
        <TabsTrigger value="b" disabled>
          Заблокировано
        </TabsTrigger>
        <TabsTrigger value="c">Доступно 2</TabsTrigger>
      </TabsList>
      <TabsContent value="a">
        <Panel>A</Panel>
      </TabsContent>
      <TabsContent value="b">
        <Panel>B</Panel>
      </TabsContent>
      <TabsContent value="c">
        <Panel>C</Panel>
      </TabsContent>
    </Tabs>
  ),
};

export const SizeSmall: Story = {
  name: 'Size: sm',
  render: () => (
    <Tabs defaultValue="a">
      <TabsList size="sm" aria-label="Small tabs">
        <TabsTrigger size="sm" value="a">
          Первый
        </TabsTrigger>
        <TabsTrigger size="sm" value="b">
          Второй
        </TabsTrigger>
      </TabsList>
      <TabsContent value="a">
        <Panel>A</Panel>
      </TabsContent>
      <TabsContent value="b">
        <Panel>B</Panel>
      </TabsContent>
    </Tabs>
  ),
};
