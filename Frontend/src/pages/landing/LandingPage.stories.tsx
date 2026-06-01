// LandingPage.stories — полный композитный рендер публичной стартовой страницы.
// Статичная композиция из 4 секций без запросов и guard'ов, поэтому одного
// Default-стори достаточно. Отдельные состояния секций покрыты секционными
// stories (HeroSection / FeaturesSection / PricingSection / FAQAccordion).
import type { Meta, StoryObj } from '@storybook/react';

import { LandingPage } from './LandingPage';

const meta: Meta<typeof LandingPage> = {
  title: 'Pages/Landing',
  component: LandingPage,
  parameters: { layout: 'fullscreen' },
};

export default meta;
type Story = StoryObj<typeof LandingPage>;

export const Default: Story = {};
