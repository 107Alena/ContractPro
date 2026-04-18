import type { Meta, StoryObj } from '@storybook/react';
import { MemoryRouter } from 'react-router-dom';

import type { ContractSummary } from '@/entities/contract';

import { RecentChecksTable } from './RecentChecksTable';

const meta: Meta<typeof RecentChecksTable> = {
  title: 'Widgets/Dashboard/RecentChecksTable',
  component: RecentChecksTable,
  tags: ['autodocs'],
  decorators: [
    (Story) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof RecentChecksTable>;

const items: ContractSummary[] = [
  {
    contract_id: 'c1',
    title: 'Договор аренды офиса',
    processing_status: 'READY',
    updated_at: '2026-04-15T09:30:00Z',
  },
  {
    contract_id: 'c2',
    title: 'Услуги IT-консалтинга',
    processing_status: 'ANALYZING',
    updated_at: '2026-04-17T10:05:00Z',
  },
  {
    contract_id: 'c3',
    title: 'NDA с подрядчиком',
    processing_status: 'AWAITING_USER_INPUT',
    updated_at: '2026-04-17T11:15:00Z',
  },
  {
    contract_id: 'c4',
    title: 'Поставка ПО',
    processing_status: 'FAILED',
    updated_at: '2026-04-16T14:00:00Z',
  },
];

export const Default: Story = { args: { items } };

export const Loading: Story = { args: { isLoading: true, items: [] } };

export const Empty: Story = { args: { items: [] } };

export const ErrorState: Story = { args: { error: new Error('network') } };
