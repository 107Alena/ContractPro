import type { Meta, StoryObj } from '@storybook/react';

import type { UserProcessingStatus } from '@/shared/api';

import { StatusBadge } from './status-badge';

const ALL_STATUSES: readonly UserProcessingStatus[] = [
  'UPLOADED',
  'QUEUED',
  'PROCESSING',
  'ANALYZING',
  'AWAITING_USER_INPUT',
  'GENERATING_REPORTS',
  'READY',
  'PARTIALLY_FAILED',
  'FAILED',
  'REJECTED',
];

const meta = {
  title: 'Entities/Version/StatusBadge',
  component: StatusBadge,
  tags: ['autodocs'],
  parameters: { layout: 'centered' },
  argTypes: {
    status: { control: 'select', options: [...ALL_STATUSES, undefined] },
  },
  args: { status: 'READY' },
} satisfies Meta<typeof StatusBadge>;
export default meta;

type Story = StoryObj<typeof meta>;

export const Uploaded: Story = { args: { status: 'UPLOADED' } };
export const Queued: Story = { args: { status: 'QUEUED' } };
export const Processing: Story = { args: { status: 'PROCESSING' } };
export const Analyzing: Story = { args: { status: 'ANALYZING' } };
export const AwaitingUserInput: Story = { args: { status: 'AWAITING_USER_INPUT' } };
export const GeneratingReports: Story = { args: { status: 'GENERATING_REPORTS' } };
export const Ready: Story = { args: { status: 'READY' } };
export const PartiallyFailed: Story = { args: { status: 'PARTIALLY_FAILED' } };
export const Failed: Story = { args: { status: 'FAILED' } };
export const Rejected: Story = { args: { status: 'REJECTED' } };
export const Unknown: Story = { args: { status: undefined } };

export const AllStatuses: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap', maxWidth: 640 }}>
      {ALL_STATUSES.map((status) => (
        <StatusBadge key={status} status={status} />
      ))}
      <StatusBadge status={undefined} />
    </div>
  ),
};
