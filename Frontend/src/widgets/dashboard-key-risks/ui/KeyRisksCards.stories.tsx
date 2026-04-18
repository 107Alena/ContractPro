import type { Meta, StoryObj } from '@storybook/react';
import { MemoryRouter } from 'react-router-dom';

import type { ContractSummary } from '@/entities/contract';

import { KeyRisksCards } from './KeyRisksCards';

const meta: Meta<typeof KeyRisksCards> = {
  title: 'Widgets/Dashboard/KeyRisksCards',
  component: KeyRisksCards,
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
type Story = StoryObj<typeof KeyRisksCards>;

const items: ContractSummary[] = [
  { contract_id: '1', title: 'Аренда', processing_status: 'READY' },
  { contract_id: '2', title: 'Услуги', processing_status: 'READY' },
  { contract_id: '3', title: 'NDA', processing_status: 'AWAITING_USER_INPUT' },
  { contract_id: '4', title: 'Поставка', processing_status: 'FAILED' },
];

export const Default: Story = { args: { items } };

export const Loading: Story = { args: { isLoading: true } };

export const Empty: Story = { args: { items: [] } };

export const ErrorState: Story = { args: { error: new Error('network') } };
