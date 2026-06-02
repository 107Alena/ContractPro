import type { Meta, StoryObj } from '@storybook/react';

import type { ContractSummary } from '@/entities/contract';

import { ContractsMetricsStrip } from './ContractsMetricsStrip';

const meta: Meta<typeof ContractsMetricsStrip> = {
  title: 'Widgets/ContractsMetricsStrip',
  component: ContractsMetricsStrip,
  parameters: { layout: 'padded' },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof ContractsMetricsStrip>;

const sample: ContractSummary[] = [
  { contract_id: '1', title: 'a', processing_status: 'READY' },
  { contract_id: '2', title: 'b', processing_status: 'ANALYZING' },
  { contract_id: '3', title: 'c', processing_status: 'AWAITING_USER_INPUT' },
  { contract_id: '4', title: 'd', processing_status: 'FAILED' },
];

export const Default: Story = {
  args: { items: sample, total: 128 },
};

export const Loading: Story = {
  args: { isLoading: true },
};

export const Empty: Story = {
  args: { items: [], total: 0 },
};

export const ErrorState: Story = {
  args: { error: new Error('network') },
};
