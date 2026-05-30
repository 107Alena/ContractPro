import type { Meta, StoryObj } from '@storybook/react';

import type { ContractSummary } from '@/entities/contract';

import { ProcessingStatus } from './ProcessingStatus';

const analyzing: ContractSummary[] = [
  { contract_id: 'c1', title: 'Договор аренды помещения', processing_status: 'ANALYZING' },
];

const meta: Meta<typeof ProcessingStatus> = {
  title: 'Widgets/Dashboard/ProcessingStatus',
  component: ProcessingStatus,
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof ProcessingStatus>;

export const Active: Story = { args: { items: analyzing } };
export const Empty: Story = {
  args: { items: [{ contract_id: 'c2', title: 'Готов', processing_status: 'READY' }] },
};
export const Loading: Story = { args: { isLoading: true } };
