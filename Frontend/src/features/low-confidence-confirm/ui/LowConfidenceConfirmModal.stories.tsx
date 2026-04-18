import type { Meta, StoryObj } from '@storybook/react';

import type { TypeConfirmationEvent } from '../model/types';
import { LowConfidenceConfirmModal } from './LowConfidenceConfirmModal';

const stub = (isPending = false): { confirm: (t: string) => void; isPending: boolean } => ({
  confirm: () => {},
  isPending,
});

const baseEvent: TypeConfirmationEvent = {
  document_id: 'c0ffee00-1111-2222-3333-444444444444',
  version_id: 'v1ee0000-aaaa-bbbb-cccc-dddddddddddd',
  status: 'AWAITING_USER_INPUT',
  suggested_type: 'услуги',
  confidence: 0.62,
  threshold: 0.75,
  alternatives: [
    { contract_type: 'подряд', confidence: 0.21 },
    { contract_type: 'NDA', confidence: 0.1 },
  ],
};

const meta = {
  title: 'Features/LowConfidenceConfirm/Modal',
  component: LowConfidenceConfirmModal,
  parameters: { layout: 'fullscreen' },
  tags: ['autodocs'],
} satisfies Meta<typeof LowConfidenceConfirmModal>;
export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    event: baseEvent,
    onDismiss: () => {},
    confirm: stub(),
  },
};

export const Pending: Story = {
  args: {
    event: baseEvent,
    onDismiss: () => {},
    confirm: stub(true),
  },
};

export const NoAlternatives: Story = {
  args: {
    event: { ...baseEvent, alternatives: [] },
    onDismiss: () => {},
    confirm: stub(),
  },
};

export const HighConfidenceGap: Story = {
  args: {
    event: {
      ...baseEvent,
      suggested_type: 'смешанный',
      confidence: 0.41,
      threshold: 0.75,
      alternatives: [
        { contract_type: 'услуги', confidence: 0.22 },
        { contract_type: 'подряд', confidence: 0.18 },
        { contract_type: 'аренда', confidence: 0.1 },
      ],
    },
    onDismiss: () => {},
    confirm: stub(),
  },
};
