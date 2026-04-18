import type { Meta, StoryObj } from '@storybook/react';

import { WhatWeCheck } from './WhatWeCheck';

const meta: Meta<typeof WhatWeCheck> = {
  title: 'Widgets/NewCheck/WhatWeCheck',
  component: WhatWeCheck,
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj<typeof WhatWeCheck>;

export const Default: Story = {
  render: () => (
    <div style={{ maxWidth: 420 }}>
      <WhatWeCheck />
    </div>
  ),
};
