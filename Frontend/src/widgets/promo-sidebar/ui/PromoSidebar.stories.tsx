// PromoSidebar.stories — бренд-колонка на Auth Page (§17.4).
import type { Meta, StoryObj } from '@storybook/react';

import { PromoSidebar } from './PromoSidebar';

const meta: Meta<typeof PromoSidebar> = {
  title: 'Widgets/PromoSidebar',
  component: PromoSidebar,
  parameters: { layout: 'fullscreen' },
};

export default meta;

type Story = StoryObj<typeof PromoSidebar>;

export const Default: Story = {
  render: () => (
    <div className="grid min-h-screen md:grid-cols-[minmax(360px,45%)_1fr]">
      <PromoSidebar />
      <div className="flex items-center justify-center bg-bg p-6 text-fg">
        <span className="text-sm text-fg-muted">← здесь — форма логина</span>
      </div>
    </div>
  ),
};
