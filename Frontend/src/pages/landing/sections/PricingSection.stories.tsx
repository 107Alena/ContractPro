import type { Meta, StoryObj } from '@storybook/react';
import { MemoryRouter } from 'react-router-dom';

import { PRICING_PLANS } from '../content';
import { PricingSection } from './PricingSection';

const meta: Meta<typeof PricingSection> = {
  title: 'Pages/Landing/PricingSection',
  component: PricingSection,
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
type Story = StoryObj<typeof PricingSection>;

export const Default: Story = {};

export const TwoPlans: Story = {
  name: '2 тарифа (Free + Pro)',
  args: {
    plans: PRICING_PLANS.filter((p) => p.id === 'free' || p.id === 'pro'),
  },
};
