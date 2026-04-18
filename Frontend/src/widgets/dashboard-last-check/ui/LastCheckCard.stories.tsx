import type { Meta, StoryObj } from '@storybook/react';
import { MemoryRouter } from 'react-router-dom';

import type { ContractSummary } from '@/entities/contract';

import { LastCheckCard } from './LastCheckCard';

const meta: Meta<typeof LastCheckCard> = {
  title: 'Widgets/Dashboard/LastCheckCard',
  component: LastCheckCard,
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
type Story = StoryObj<typeof LastCheckCard>;

const readyContract: ContractSummary = {
  contract_id: '00000000-0000-0000-0000-000000000001',
  title: 'Договор оказания услуг',
  processing_status: 'READY',
  current_version_number: 3,
  created_at: '2026-04-10T10:00:00Z',
  updated_at: '2026-04-12T12:00:00Z',
};

const processingContract: ContractSummary = {
  contract_id: '00000000-0000-0000-0000-000000000002',
  title: 'Поставка оборудования',
  processing_status: 'ANALYZING',
  current_version_number: 1,
};

export const Default: Story = {
  args: { contract: readyContract },
};

export const Processing: Story = {
  args: { contract: processingContract },
};

export const Loading: Story = {
  args: { isLoading: true },
};

export const Empty: Story = {
  args: {},
};

export const ErrorState: Story = {
  args: { error: new Error('network') },
};
