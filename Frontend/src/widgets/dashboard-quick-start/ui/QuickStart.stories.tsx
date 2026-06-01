import type { Meta, StoryObj } from '@storybook/react';

import { QuickStart } from './QuickStart';

const meta: Meta<typeof QuickStart> = {
  title: 'Widgets/Dashboard/QuickStart',
  component: QuickStart,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof QuickStart>;

export const Default: Story = {};
