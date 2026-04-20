import type { Meta, StoryObj } from '@storybook/react';

import type { ContractSummary } from '@/entities/contract';

import { ReportsMetrics } from './ReportsMetrics';

const meta: Meta<typeof ReportsMetrics> = {
  title: 'Widgets/ReportsMetrics',
  component: ReportsMetrics,
  parameters: { layout: 'padded' },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof ReportsMetrics>;

const NOW = Date.parse('2026-04-20T12:00:00Z');

const sample: ContractSummary[] = [
  {
    contract_id: 'c1',
    title: 'Договор оказания услуг',
    status: 'ACTIVE',
    current_version_number: 2,
    processing_status: 'READY',
    updated_at: '2026-04-19T10:00:00Z',
    created_at: '2026-04-01T10:00:00Z',
  },
  {
    contract_id: 'c2',
    title: 'NDA',
    status: 'ACTIVE',
    current_version_number: 1,
    processing_status: 'READY',
    updated_at: '2026-04-18T10:00:00Z',
    created_at: '2026-04-02T10:00:00Z',
  },
  {
    contract_id: 'c3',
    title: 'Договор аренды',
    status: 'ACTIVE',
    current_version_number: 1,
    processing_status: 'PARTIALLY_FAILED',
    updated_at: '2026-04-01T10:00:00Z',
    created_at: '2026-03-20T10:00:00Z',
  },
];

export const Populated: Story = {
  args: { items: sample, total: 42, now: NOW },
};

export const Loading: Story = {
  args: { isLoading: true },
};

export const Error: Story = {
  args: { error: new globalThis.Error('Network error') },
};
