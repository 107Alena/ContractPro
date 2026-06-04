import type { Meta, StoryObj } from '@storybook/react';

import type { ContractSummary } from '@/entities/contract';

import { ReportDetailPanel } from './ReportDetailPanel';

const meta: Meta<typeof ReportDetailPanel> = {
  title: 'Widgets/ReportDetailPanel',
  component: ReportDetailPanel,
  parameters: { layout: 'padded' },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof ReportDetailPanel>;

const contract: ContractSummary = {
  contract_id: 'c1',
  title: 'Договор оказания консультационных услуг №ПК-2026/147',
  status: 'ACTIVE',
  current_version_number: 3,
  processing_status: 'READY',
  updated_at: '2026-04-19T14:20:00Z',
  created_at: '2026-04-01T10:00:00Z',
};

export const Open: Story = {
  args: {
    contract,
    versionId: 'ver-uuid-1',
    showRisk: true,
    riskProfile: { level: 'medium', high: 2, medium: 3, low: 1 },
    onClose: () => {},
    onOpenShare: () => {},
  },
};

export const RiskLoading: Story = {
  args: {
    contract,
    versionId: 'ver-uuid-1',
    showRisk: true,
    riskLoading: true,
    onClose: () => {},
    onOpenShare: () => {},
  },
};

export const RiskUnavailable: Story = {
  args: {
    contract,
    versionId: 'ver-uuid-1',
    showRisk: true,
    riskProfile: null,
    onClose: () => {},
    onOpenShare: () => {},
  },
};

export const Empty: Story = {
  args: { contract: null, onClose: () => {} },
};

export const WithWarnings: Story = {
  args: {
    contract: { ...contract, processing_status: 'PARTIALLY_FAILED' },
    onClose: () => {},
    onOpenShare: () => {},
  },
};
