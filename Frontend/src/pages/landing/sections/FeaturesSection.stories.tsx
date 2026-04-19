import type { Meta, StoryObj } from '@storybook/react';

import { FEATURES } from '../content';
import { FeaturesSection } from './FeaturesSection';

const meta: Meta<typeof FeaturesSection> = {
  title: 'Pages/Landing/FeaturesSection',
  component: FeaturesSection,
  tags: ['autodocs'],
  parameters: { layout: 'fullscreen' },
};

export default meta;
type Story = StoryObj<typeof FeaturesSection>;

export const Default: Story = {};

export const ThreeItems: Story = {
  name: '3 features (минимум)',
  args: { items: FEATURES.slice(0, 3) },
};
