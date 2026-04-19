import type { Meta, StoryObj } from '@storybook/react';

import { FAQ_ITEMS } from '../content';
import { FAQAccordion } from './FAQAccordion';

const meta: Meta<typeof FAQAccordion> = {
  title: 'Pages/Landing/FAQAccordion',
  component: FAQAccordion,
  tags: ['autodocs'],
  parameters: { layout: 'fullscreen' },
};

export default meta;
type Story = StoryObj<typeof FAQAccordion>;

export const Default: Story = {};

export const ShortList: Story = {
  name: '3 вопроса',
  args: { items: FAQ_ITEMS.slice(0, 3) },
};
