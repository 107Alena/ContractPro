import type { Meta, StoryObj } from '@storybook/react';

import type { ContractSummary } from '@/entities/contract';

import { RecentChecksTable } from './RecentChecksTable';

const meta: Meta<typeof RecentChecksTable> = {
  title: 'Widgets/Dashboard/RecentChecksTable',
  component: RecentChecksTable,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof RecentChecksTable>;

const items: ContractSummary[] = [
  {
    contract_id: 'c1',
    title: 'Договор аренды офиса',
    status: 'ACTIVE',
    processing_status: 'READY',
    contract_type: 'LEASE',
    risk_level: 'high',
    risk_counts: { high: 2, medium: 1, low: 0 },
    created_at: '2026-04-14T09:30:00Z',
    updated_at: '2026-04-15T09:30:00Z',
  },
  {
    contract_id: 'c2',
    title: 'Услуги IT-консалтинга',
    status: 'ACTIVE',
    processing_status: 'ANALYZING',
    contract_type: 'SERVICES',
    risk_level: null,
    risk_counts: null,
    created_at: '2026-04-17T10:05:00Z',
    updated_at: '2026-04-17T10:05:00Z',
  },
  {
    contract_id: 'c3',
    title: 'NDA с подрядчиком',
    status: 'ACTIVE',
    processing_status: 'AWAITING_USER_INPUT',
    contract_type: 'NDA',
    risk_level: null,
    risk_counts: null,
    created_at: '2026-04-17T11:15:00Z',
    updated_at: '2026-04-17T11:15:00Z',
  },
  {
    contract_id: 'c4',
    title: 'Поставка ПО',
    status: 'ACTIVE',
    processing_status: 'FAILED',
    contract_type: 'SUPPLY',
    risk_level: null,
    risk_counts: null,
    created_at: '2026-04-16T14:00:00Z',
    updated_at: '2026-04-16T14:00:00Z',
  },
];

export const Default: Story = { args: { items } };

export const Loading: Story = { args: { isLoading: true, items: [] } };

export const Empty: Story = { args: { items: [] } };

export const ErrorState: Story = { args: { error: new Error('network') } };
