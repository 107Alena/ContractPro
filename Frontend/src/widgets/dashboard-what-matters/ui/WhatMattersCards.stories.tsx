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

const sample: ContractSummary[] = [
  { contract_id: '1', title: 'Аренда офиса', processing_status: 'READY' },
  { contract_id: '2', title: 'Услуги консалтинга', processing_status: 'ANALYZING' },
  { contract_id: '3', title: 'NDA партнёр', processing_status: 'AWAITING_USER_INPUT' },
  { contract_id: '4', title: 'Поставка ПО', processing_status: 'FAILED' },
  { contract_id: '5', title: 'Договор подряда', processing_status: 'READY' },
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
