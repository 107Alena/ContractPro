import type { Meta, StoryObj } from '@storybook/react';

import { WillHappenSteps } from './WillHappenSteps';

const meta: Meta<typeof WillHappenSteps> = {
  title: 'Widgets/NewCheck/WillHappenSteps',
  component: WillHappenSteps,
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj<typeof WillHappenSteps>;

export const Default: Story = {
  render: () => (
    <div style={{ maxWidth: 420 }}>
      <WillHappenSteps />
    </div>
  ),
};
