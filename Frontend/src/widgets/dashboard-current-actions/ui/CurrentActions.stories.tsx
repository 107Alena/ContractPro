import type { Meta, StoryObj } from '@storybook/react';

import type { ContractSummary } from '@/entities/contract';

import { CurrentActions } from './CurrentActions';

const base = {
  status: 'ACTIVE' as const,
  created_at: '2026-04-15T10:00:00Z',
  updated_at: '2026-04-15T10:00:00Z',
};

const sample: ContractSummary[] = [
  { ...base, contract_id: '1', title: 'Договор аренды помещения', processing_status: 'ANALYZING' },
  {
    ...base,
    contract_id: '2',
    title: 'Лицензионное соглашение',
    processing_status: 'AWAITING_USER_INPUT',
  },
  { ...base, contract_id: '3', title: 'Договор поставки №128', processing_status: 'FAILED' },
];

const meta: Meta<typeof CurrentActions> = {
  title: 'Widgets/Dashboard/CurrentActions',
  component: CurrentActions,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof CurrentActions>;

export const Default: Story = { args: { items: sample } };
export const Empty: Story = {
  args: { items: [{ ...base, contract_id: 'r', title: 'Готово', processing_status: 'READY' }] },
};
export const Loading: Story = { args: { isLoading: true } };
export const ErrorState: Story = { args: { error: new Error('network') } };
