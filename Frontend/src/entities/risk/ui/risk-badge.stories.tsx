import type { Meta, StoryObj } from '@storybook/react';

import { TooltipProvider } from '@/shared/ui';

import { RISK_LEVELS } from '../model';
import { RiskBadge } from './risk-badge';

const meta = {
  title: 'Entities/Risk/RiskBadge',
  component: RiskBadge,
  tags: ['autodocs'],
  parameters: { layout: 'centered' },
  argTypes: {
    level: { control: 'inline-radio', options: RISK_LEVELS },
    showTooltip: { control: 'boolean' },
  },
  args: { level: 'high', showTooltip: false },
  decorators: [
    (Story) => (
      <TooltipProvider delayDuration={200}>
        <div className="p-8">
          <Story />
        </div>
      </TooltipProvider>
    ),
  ],
} satisfies Meta<typeof RiskBadge>;
export default meta;

type Story = StoryObj<typeof meta>;

export const High: Story = { args: { level: 'high' } };
export const Medium: Story = { args: { level: 'medium' } };
export const Low: Story = { args: { level: 'low' } };

export const WithTooltip: Story = {
  args: { level: 'high', showTooltip: true },
  parameters: {
    docs: {
      description: {
        story:
          'Tooltip с legend-описанием уровня риска. Используется в заголовках/легендах; ' +
          'в табличных списках рекомендуется оставлять `showTooltip={false}` — см. header ' +
          '`risk-badge.tsx`.',
      },
    },
  },
};

export const AllLevels: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
      <RiskBadge level="high" />
      <RiskBadge level="medium" />
      <RiskBadge level="low" />
    </div>
  ),
};

export const AllLevelsWithTooltip: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
      <RiskBadge level="high" showTooltip />
      <RiskBadge level="medium" showTooltip />
      <RiskBadge level="low" showTooltip />
    </div>
  ),
};
