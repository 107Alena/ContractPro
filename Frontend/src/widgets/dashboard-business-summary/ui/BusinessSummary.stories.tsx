import type { Meta, StoryObj } from '@storybook/react';

import { BusinessSummary } from './BusinessSummary';

const meta: Meta<typeof BusinessSummary> = {
  title: 'Widgets/Dashboard/BusinessSummary',
  component: BusinessSummary,
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof BusinessSummary>;

export const Default: Story = { args: { total: 12, inProgress: 3 } };
/** stats недоступны → «в работе» = «—», «проверено» из /contracts. */
export const NoStats: Story = { args: { total: 12 } };
export const Loading: Story = { args: { isLoading: true } };
export const ErrorState: Story = { args: { error: new Error('net') } };
