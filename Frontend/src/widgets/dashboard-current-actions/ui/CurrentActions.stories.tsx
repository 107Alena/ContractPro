import type { Meta, StoryObj } from '@storybook/react';
import { MemoryRouter } from 'react-router-dom';

import type { ContractSummary } from '@/entities/contract';

import { CurrentActions } from './CurrentActions';

const sample: ContractSummary[] = [
  { contract_id: '1', title: 'Договор аренды помещения', processing_status: 'ANALYZING' },
  { contract_id: '2', title: 'Лицензионное соглашение', processing_status: 'AWAITING_USER_INPUT' },
  { contract_id: '3', title: 'Договор поставки №128', processing_status: 'FAILED' },
];

const meta: Meta<typeof CurrentActions> = {
  title: 'Widgets/Dashboard/CurrentActions',
  component: CurrentActions,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
  decorators: [
    (Story) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof CurrentActions>;

export const Default: Story = { args: { items: sample } };
export const Empty: Story = {
  args: { items: [{ contract_id: 'r', title: 'Готово', processing_status: 'READY' }] },
};
export const Loading: Story = { args: { isLoading: true } };
export const ErrorState: Story = { args: { error: new Error('network') } };
