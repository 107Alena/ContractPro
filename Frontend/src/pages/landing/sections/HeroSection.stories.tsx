import type { Meta, StoryObj } from '@storybook/react';
import { MemoryRouter } from 'react-router-dom';

import { HERO_CONTENT } from '../content';
import { HeroSection } from './HeroSection';

const meta: Meta<typeof HeroSection> = {
  title: 'Pages/Landing/HeroSection',
  component: HeroSection,
  tags: ['autodocs'],
  parameters: { layout: 'fullscreen' },
  decorators: [
    (Story) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof HeroSection>;

export const Default: Story = {};

export const ShortCopy: Story = {
  name: 'Short copy (без trust-бейджей)',
  args: {
    content: {
      ...HERO_CONTENT,
      title: 'Договоры под контролем ИИ',
      subtitle: 'Анализ за минуту. Без настройки.',
      trustBadges: [],
    },
  },
};
