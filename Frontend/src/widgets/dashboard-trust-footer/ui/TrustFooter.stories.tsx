import type { Meta, StoryObj } from '@storybook/react';

import { TrustFooter } from './TrustFooter';

const meta: Meta<typeof TrustFooter> = {
  title: 'Widgets/Dashboard/TrustFooter',
  component: TrustFooter,
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof TrustFooter>;

export const Default: Story = {};
