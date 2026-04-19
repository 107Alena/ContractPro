import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import { SegmentedControl, type SegmentedControlOption } from './segmented-control';

const meta = {
  title: 'Shared/SegmentedControl',
  tags: ['autodocs'],
  parameters: { layout: 'centered' },
} satisfies Meta;
export default meta;

type Story = StoryObj<typeof meta>;

type Status = 'all' | 'active' | 'archived';
const STATUS_OPTIONS: ReadonlyArray<SegmentedControlOption<Status>> = [
  { value: 'all', label: 'Все' },
  { value: 'active', label: 'Активные' },
  { value: 'archived', label: 'Архив' },
];

function DefaultDemo() {
  const [value, setValue] = useState<Status>('all');
  return (
    <SegmentedControl<Status>
      options={STATUS_OPTIONS}
      value={value}
      onValueChange={setValue}
      ariaLabel="Статус документа"
    />
  );
}

export const Default: Story = {
  render: () => <DefaultDemo />,
};

const ICON_OPTIONS: ReadonlyArray<SegmentedControlOption<'list' | 'grid'>> = [
  {
    value: 'list',
    label: 'Список',
    icon: (
      <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
        <path d="M3 4h10M3 8h10M3 12h10" stroke="currentColor" strokeWidth="1.5" />
      </svg>
    ),
  },
  {
    value: 'grid',
    label: 'Плитка',
    icon: (
      <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
        <rect x="3" y="3" width="4" height="4" stroke="currentColor" />
        <rect x="9" y="3" width="4" height="4" stroke="currentColor" />
        <rect x="3" y="9" width="4" height="4" stroke="currentColor" />
        <rect x="9" y="9" width="4" height="4" stroke="currentColor" />
      </svg>
    ),
  },
];

function WithIconsDemo() {
  const [value, setValue] = useState<'list' | 'grid'>('list');
  return (
    <SegmentedControl
      options={ICON_OPTIONS}
      value={value}
      onValueChange={setValue}
      ariaLabel="Режим отображения"
    />
  );
}

export const WithIcons: Story = { render: () => <WithIconsDemo /> };

function DisabledOptionDemo() {
  const [value, setValue] = useState<Status>('all');
  const opts: ReadonlyArray<SegmentedControlOption<Status>> = [
    { value: 'all', label: 'Все' },
    { value: 'active', label: 'Активные', disabled: true },
    { value: 'archived', label: 'Архив' },
  ];
  return (
    <SegmentedControl<Status>
      options={opts}
      value={value}
      onValueChange={setValue}
      ariaLabel="Статус (одна заблокирована)"
    />
  );
}
export const DisabledOption: Story = { render: () => <DisabledOptionDemo /> };

function FullyDisabledDemo() {
  return (
    <SegmentedControl<Status>
      options={STATUS_OPTIONS}
      value="all"
      onValueChange={() => {}}
      ariaLabel="Статус (полностью заблокирован)"
      disabled
    />
  );
}
export const FullyDisabled: Story = { render: () => <FullyDisabledDemo /> };

function SmallDemo() {
  const [value, setValue] = useState<Status>('all');
  return (
    <SegmentedControl<Status>
      options={STATUS_OPTIONS}
      value={value}
      onValueChange={setValue}
      ariaLabel="Статус small"
      size="sm"
    />
  );
}
export const SizeSmall: Story = { name: 'Size: sm', render: () => <SmallDemo /> };

function FullWidthDemo() {
  const [value, setValue] = useState<Status>('all');
  return (
    <div className="w-96">
      <SegmentedControl<Status>
        options={STATUS_OPTIONS}
        value={value}
        onValueChange={setValue}
        ariaLabel="Статус fullWidth"
        fullWidth
      />
    </div>
  );
}
export const FullWidth: Story = { render: () => <FullWidthDemo /> };
