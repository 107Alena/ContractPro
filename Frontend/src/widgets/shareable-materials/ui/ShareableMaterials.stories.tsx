import type { Meta, StoryObj } from '@storybook/react';

import { ShareableMaterials } from './ShareableMaterials';

const meta: Meta<typeof ShareableMaterials> = {
  title: 'Widgets/Reports/ShareableMaterials',
  component: ShareableMaterials,
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof ShareableMaterials>;

export const Default: Story = {};
