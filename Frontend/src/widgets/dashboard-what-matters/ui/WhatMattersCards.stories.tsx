import type { Meta, StoryObj } from '@storybook/react';

import type { ContractSummary } from '@/entities/contract';

import { WhatMattersCards } from './WhatMattersCards';

const meta: Meta<typeof WhatMattersCards> = {
  title: 'Widgets/Dashboard/WhatMattersCards',
  component: WhatMattersCards,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof WhatMattersCards>;

const base = {
  status: 'ACTIVE' as const,
  created_at: '2026-04-15T10:00:00Z',
  updated_at: '2026-04-15T10:00:00Z',
};

const sample: ContractSummary[] = [
  { ...base, contract_id: '1', title: 'Аренда офиса', processing_status: 'READY' },
  { ...base, contract_id: '2', title: 'Услуги консалтинга', processing_status: 'ANALYZING' },
  { ...base, contract_id: '3', title: 'NDA партнёр', processing_status: 'AWAITING_USER_INPUT' },
  { ...base, contract_id: '4', title: 'Поставка ПО', processing_status: 'FAILED' },
  { ...base, contract_id: '5', title: 'Договор подряда', processing_status: 'READY' },
];

export const Default: Story = {
  args: { items: sample, total: 12 },
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
