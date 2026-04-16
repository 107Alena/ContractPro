import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import { Chip, type ChipProps } from './chip';

const meta = {
  title: 'Shared/Chip',
  component: Chip,
  tags: ['autodocs'],
  args: { children: 'Фильтр' },
} satisfies Meta<typeof Chip>;
export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const Selected: Story = { args: { selected: true } };
export const Interactive: Story = { args: { interactive: true } };

function RemovableChip(args: ChipProps) {
  const [visible, setVisible] = useState(true);
  if (!visible) {
    return <span style={{ color: 'var(--color-fg-muted)' }}>(удалено)</span>;
  }
  return <Chip {...args} onRemove={() => setVisible(false)} />;
}

export const WithRemove: Story = {
  args: {
    children: 'Высокий риск',
    removeLabel: 'Удалить фильтр «Высокий риск»',
  },
  render: (args) => <RemovableChip {...args} />,
};

export const FilterChipsGroup: Story = {
  render: () => {
    const items = [
      { key: 'high', label: 'Высокий риск' },
      { key: 'medium', label: 'Средний риск' },
      { key: 'archived', label: 'В архиве' },
    ];
    return (
      <div style={{ display: 'flex', gap: 'var(--space-2)', flexWrap: 'wrap' }}>
        {items.map((it, i) => (
          <Chip key={it.key} selected={i === 0} interactive>
            {it.label}
          </Chip>
        ))}
      </div>
    );
  },
};
