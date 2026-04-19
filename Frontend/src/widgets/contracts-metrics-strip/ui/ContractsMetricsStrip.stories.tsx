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
  { contract_id: '1', title: 'a', status: 'ACTIVE' },
  { contract_id: '2', title: 'b', status: 'ACTIVE' },
  { contract_id: '3', title: 'c', status: 'ARCHIVED' },
  { contract_id: '4', title: 'd', status: 'DELETED' },
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
