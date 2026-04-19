import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import { SearchInput } from './SearchInput';

const meta = {
  title: 'Features/Search/SearchInput',
  component: SearchInput,
  parameters: { layout: 'centered' },
  tags: ['autodocs'],
  args: {
    value: '',
    onValueChange: () => undefined,
  },
} satisfies Meta<typeof SearchInput>;
export default meta;

type Story = StoryObj<typeof meta>;

function Harness({
  initialValue = '',
  isPending = false,
  disabled = false,
  clearable = true,
}: {
  initialValue?: string;
  isPending?: boolean;
  disabled?: boolean;
  clearable?: boolean;
}) {
  const [value, setValue] = useState(initialValue);
  return (
    <div className="w-[320px]">
      <SearchInput
        value={value}
        onValueChange={setValue}
        placeholder="Поиск по договорам…"
        isPending={isPending}
        disabled={disabled}
        clearable={clearable}
      />
    </div>
  );
}

export const Default: Story = {
  render: () => <Harness />,
};

export const Filled: Story = {
  render: () => <Harness initialValue="Договор поставки" />,
};

export const Pending: Story = {
  render: () => <Harness initialValue="поиск…" isPending />,
};

export const NotClearable: Story = {
  render: () => <Harness initialValue="fixed" clearable={false} />,
};

export const Disabled: Story = {
  render: () => <Harness initialValue="readonly" disabled />,
};
