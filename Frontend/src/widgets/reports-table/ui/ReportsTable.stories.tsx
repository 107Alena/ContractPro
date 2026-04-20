import type { Meta, StoryObj } from '@storybook/react';
import { MemoryRouter } from 'react-router-dom';

import type { ContractSummary } from '@/entities/contract';

import { ReportsTable } from './ReportsTable';

const meta: Meta<typeof ReportsTable> = {
  title: 'Widgets/ReportsTable',
  component: ReportsTable,
  decorators: [
    (Story) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
  parameters: { layout: 'padded' },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof ReportsTable>;

const sample: ContractSummary[] = [
  {
    contract_id: 'c1',
    title: 'Договор оказания услуг',
    status: 'ACTIVE',
    current_version_number: 2,
    processing_status: 'READY',
    updated_at: '2026-04-19T10:00:00Z',
  },
  {
    contract_id: 'c2',
    title: 'NDA с ООО «Бета»',
    status: 'ACTIVE',
    current_version_number: 1,
    processing_status: 'READY',
    updated_at: '2026-04-18T10:00:00Z',
  },
  {
    contract_id: 'c3',
    title: 'Договор аренды офиса',
    status: 'ACTIVE',
    current_version_number: 1,
    processing_status: 'PARTIALLY_FAILED',
    updated_at: '2026-04-12T09:30:00Z',
  },
];

export const Populated: Story = {
  args: { items: sample },
};

export const Selected: Story = {
  args: { items: sample, selectedId: 'c2' },
};

export const Loading: Story = {
  args: { items: [], isLoading: true },
};

export const Empty: Story = {
  args: { items: [] },
};

export const FilteredEmpty: Story = {
  args: { items: [], hasActiveFilters: true },
};

export const Error: Story = {
  args: { items: [], error: new globalThis.Error('Сервис временно недоступен') },
};
