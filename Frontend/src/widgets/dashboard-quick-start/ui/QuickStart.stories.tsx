import type { Meta, StoryObj } from '@storybook/react';
import { MemoryRouter } from 'react-router-dom';

import { QuickStart } from './QuickStart';

const meta: Meta<typeof QuickStart> = {
  title: 'Widgets/Dashboard/QuickStart',
  component: QuickStart,
  tags: ['autodocs'],
  decorators: [
    (Story) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof QuickStart>;

export const Default: Story = {};
