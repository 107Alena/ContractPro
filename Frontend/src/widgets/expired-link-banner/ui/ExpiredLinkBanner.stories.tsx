import type { Meta, StoryObj } from '@storybook/react';

import { ExpiredLinkBanner } from './ExpiredLinkBanner';

const meta: Meta<typeof ExpiredLinkBanner> = {
  title: 'Widgets/ExpiredLinkBanner',
  component: ExpiredLinkBanner,
  parameters: { layout: 'padded' },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof ExpiredLinkBanner>;

export const Visible: Story = {
  args: { visible: true, onDismiss: () => {} },
};

export const WithRetry: Story = {
  args: { visible: true, onDismiss: () => {}, onRequestNew: () => {} },
};

export const Hidden: Story = {
  args: { visible: false, onDismiss: () => {} },
};
