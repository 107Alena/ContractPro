import type { Meta, StoryObj } from '@storybook/react';

import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from './accordion';

const meta = {
  title: 'Shared/Accordion',
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
} satisfies Meta;
export default meta;

type Story = StoryObj<typeof meta>;

export const Single: Story = {
  name: 'type="single" (по умолчанию одна открыта)',
  render: () => (
    <Accordion type="single" collapsible defaultValue="1">
      <AccordionItem value="1">
        <AccordionTrigger>Что такое риск-профиль?</AccordionTrigger>
        <AccordionContent>
          Риск-профиль — обобщённая оценка договора по шкале от 0 до 100, учитывающая
          высокие/средние/низкие риски.
        </AccordionContent>
      </AccordionItem>
      <AccordionItem value="2">
        <AccordionTrigger>Обязательно ли загружать PDF?</AccordionTrigger>
        <AccordionContent>
          В v1 поддерживается только PDF (до 20 МБ и 100 страниц). DOC/DOCX в дорожной карте.
        </AccordionContent>
      </AccordionItem>
      <AccordionItem value="3">
        <AccordionTrigger>Как хранятся документы?</AccordionTrigger>
        <AccordionContent>
          Оригиналы шифруются и хранятся в Object Storage. Удаление — по запросу.
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  ),
};

export const Multiple: Story = {
  name: 'type="multiple" (несколько открытых)',
  render: () => (
    <Accordion type="multiple" defaultValue={['1', '2']}>
      <AccordionItem value="1">
        <AccordionTrigger>Раздел 1</AccordionTrigger>
        <AccordionContent>Контент раздела 1.</AccordionContent>
      </AccordionItem>
      <AccordionItem value="2">
        <AccordionTrigger>Раздел 2</AccordionTrigger>
        <AccordionContent>Контент раздела 2.</AccordionContent>
      </AccordionItem>
      <AccordionItem value="3">
        <AccordionTrigger>Раздел 3</AccordionTrigger>
        <AccordionContent>Контент раздела 3.</AccordionContent>
      </AccordionItem>
    </Accordion>
  ),
};

export const Ghost: Story = {
  name: 'variant="ghost" (без разделителей)',
  render: () => (
    <Accordion type="single" collapsible>
      <AccordionItem variant="ghost" value="1">
        <AccordionTrigger>Без границ 1</AccordionTrigger>
        <AccordionContent>Контент без границ.</AccordionContent>
      </AccordionItem>
      <AccordionItem variant="ghost" value="2">
        <AccordionTrigger>Без границ 2</AccordionTrigger>
        <AccordionContent>Контент без границ.</AccordionContent>
      </AccordionItem>
    </Accordion>
  ),
};

export const Disabled: Story = {
  render: () => (
    <Accordion type="single" collapsible>
      <AccordionItem value="1">
        <AccordionTrigger>Доступен</AccordionTrigger>
        <AccordionContent>Контент.</AccordionContent>
      </AccordionItem>
      <AccordionItem value="2">
        <AccordionTrigger disabled>Заблокирован</AccordionTrigger>
        <AccordionContent>Никогда не увидите.</AccordionContent>
      </AccordionItem>
    </Accordion>
  ),
};

export const DefaultOpen: Story = {
  name: 'defaultOpen (для axe)',
  render: () => (
    <Accordion type="single" defaultValue="1">
      <AccordionItem value="1">
        <AccordionTrigger>Открыт изначально</AccordionTrigger>
        <AccordionContent>Параграф открыт при монтировании — для a11y-snapshot.</AccordionContent>
      </AccordionItem>
    </Accordion>
  ),
};

export const SizeSmall: Story = {
  name: 'size="sm"',
  render: () => (
    <Accordion type="single" collapsible>
      <AccordionItem value="1">
        <AccordionTrigger size="sm">Компактный</AccordionTrigger>
        <AccordionContent>Маленький контент.</AccordionContent>
      </AccordionItem>
      <AccordionItem value="2">
        <AccordionTrigger size="sm">Ещё один</AccordionTrigger>
        <AccordionContent>Маленький контент 2.</AccordionContent>
      </AccordionItem>
    </Accordion>
  ),
};
