import type { Meta, StoryObj } from '@storybook/react';

import type { ContractSummary } from '@/entities/contract';

import { DocumentsTable } from './DocumentsTable';

const meta: Meta<typeof DocumentsTable> = {
  title: 'Widgets/DocumentsTable',
  component: DocumentsTable,
  parameters: { layout: 'padded' },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof DocumentsTable>;

const sample: ContractSummary[] = [
  {
    contract_id: 'c1',
    title: 'Договор оказания услуг',
    status: 'ACTIVE',
    current_version_number: 2,
    processing_status: 'READY',
    contract_type: 'SERVICES',
    risk_level: 'high',
    risk_counts: { high: 2, medium: 1, low: 1 },
    created_at: '2026-04-15T10:00:00Z',
    updated_at: '2026-04-16T14:20:00Z',
  },
  {
    contract_id: 'c2',
    title: 'NDA с ООО «Бета»',
    status: 'ARCHIVED',
    current_version_number: 1,
    processing_status: 'READY',
    contract_type: 'NDA',
    risk_level: 'low',
    risk_counts: { high: 0, medium: 0, low: 2 },
    created_at: '2026-04-09T10:00:00Z',
    updated_at: '2026-04-10T10:00:00Z',
  },
  {
    contract_id: 'c3',
    title: 'Договор аренды офиса',
    status: 'ACTIVE',
    current_version_number: 1,
    processing_status: 'ANALYZING',
    contract_type: 'LEASE',
    risk_level: null,
    risk_counts: null,
    created_at: '2026-04-18T09:30:00Z',
    updated_at: '2026-04-18T09:30:00Z',
  },
  {
    contract_id: 'c4',
    title: 'Поставка оборудования',
    status: 'ACTIVE',
    current_version_number: 3,
    processing_status: 'PARTIALLY_FAILED',
    contract_type: 'SUPPLY',
    risk_level: 'medium',
    risk_counts: { high: 0, medium: 2, low: 3 },
    created_at: '2026-04-19T11:00:00Z',
    updated_at: '2026-04-19T11:00:00Z',
  },
];

export const Default: Story = {
  args: { items: sample },
};

export const Loading: Story = {
  args: { items: [], isLoading: true },
};

export const Empty: Story = {
  args: { items: [] },
};

export const EmptyFiltered: Story = {
  args: { items: [], hasActiveFilters: true },
};

export const ErrorState: Story = {
  args: { items: [], error: new Error('Network timeout') },
};

export const WithRowActions: Story = {
  args: {
    items: sample,
    renderRowActions: () => (
      <div className="flex gap-1">
        <button className="rounded border border-border px-2 py-1 text-xs">Архив</button>
        <button className="rounded border border-border px-2 py-1 text-xs">Удалить</button>
      </div>
    ),
  },
};

export const Fetching: Story = {
  args: { items: sample, isFetching: true },
};

const manyRows: ContractSummary[] = Array.from({ length: 120 }, (_, i) => ({
  contract_id: `bulk-${i}`,
  title: `Договор №${i + 1}`,
  status: 'ACTIVE' as const,
  current_version_number: 1,
  processing_status: 'READY' as const,
  contract_type: 'OTHER' as const,
  risk_level: 'medium' as const,
  risk_counts: { high: 0, medium: 1, low: 0 },
  created_at: '2026-04-19T10:00:00Z',
  updated_at: '2026-04-19T10:00:00Z',
}));

export const Virtualized: Story = {
  args: { items: manyRows },
};

export const RoleRestricted: Story = {
  args: { items: sample },
  parameters: {
    docs: {
      description: {
        story: 'BUSINESS_USER — колонка «Действия» отсутствует (renderRowActions не передан).',
      },
    },
  },
};
