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

const base = {
  status: 'ACTIVE' as const,
  created_at: '2026-04-15T10:00:00Z',
  updated_at: '2026-04-15T10:00:00Z',
};

const sample: ContractSummary[] = [
  { ...base, contract_id: '1', title: 'a', processing_status: 'READY', risk_level: 'high' },
  { ...base, contract_id: '2', title: 'b', processing_status: 'ANALYZING', risk_level: null },
  {
    ...base,
    contract_id: '3',
    title: 'c',
    processing_status: 'AWAITING_USER_INPUT',
    risk_level: null,
  },
  { ...base, contract_id: '4', title: 'd', processing_status: 'FAILED', risk_level: null },
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
