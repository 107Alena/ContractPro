import type { Meta, StoryObj } from '@storybook/react';

import { Tips } from './Tips';

const meta: Meta<typeof Tips> = {
  title: 'Widgets/NewCheck/Tips',
  component: Tips,
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof Tips>;

export const Default: Story = {};
