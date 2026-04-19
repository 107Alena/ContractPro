import type { Meta, StoryObj } from '@storybook/react';
import { useEffect, useState } from 'react';

import { SearchInput } from './search-input';

const meta = {
  title: 'Shared/SearchInput',
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
  debounceMs = 0,
}: {
  initialValue?: string;
  isPending?: boolean;
  disabled?: boolean;
  clearable?: boolean;
  debounceMs?: number;
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
        debounceMs={debounceMs}
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

function DebouncedHarness() {
  const [inputValue, setInputValue] = useState('');
  const [debouncedValue, setDebouncedValue] = useState('');
  const [pending, setPending] = useState(false);

  useEffect(() => {
    setPending(inputValue !== debouncedValue);
  }, [inputValue, debouncedValue]);

  return (
    <div className="flex w-[360px] flex-col gap-2">
      <SearchInput
        value={debouncedValue}
        onValueChange={(v) => {
          setDebouncedValue(v);
        }}
        onInputChange={(v) => setInputValue(v)}
        debounceMs={300}
        isPending={pending}
        placeholder="Поиск с debounce 300мс"
      />
      <div className="text-xs text-fg-muted">
        input: <code>{JSON.stringify(inputValue)}</code> · debounced:{' '}
        <code>{JSON.stringify(debouncedValue)}</code>
      </div>
    </div>
  );
}

export const Debounced: Story = {
  name: 'Debounced (300ms)',
  render: () => <DebouncedHarness />,
};
