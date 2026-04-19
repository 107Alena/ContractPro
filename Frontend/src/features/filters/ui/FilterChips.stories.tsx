import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import type { FilterDefinition, FilterGroupValue, FilterValue } from '../model/types';
import { FilterChips } from './FilterChips';

const DEFS: readonly FilterDefinition[] = [
  {
    key: 'status',
    label: 'Статус',
    kind: 'single',
    options: [
      { value: 'ACTIVE', label: 'Активные' },
      { value: 'ARCHIVED', label: 'В архиве' },
      { value: 'DELETED', label: 'Удалённые' },
    ],
  },
  {
    key: 'type',
    label: 'Тип договора',
    kind: 'multi',
    pinned: false,
    options: [
      { value: 'SUPPLY', label: 'Поставка' },
      { value: 'SERVICE', label: 'Услуги' },
      { value: 'LICENSE', label: 'Лицензия' },
    ],
  },
];

const meta = {
  title: 'Features/Filters/FilterChips',
  component: FilterChips,
  parameters: { layout: 'padded' },
  tags: ['autodocs'],
  args: {
    definitions: DEFS,
    values: { status: '', type: [] },
    onToggleOption: () => undefined,
    onClear: () => undefined,
  },
} satisfies Meta<typeof FilterChips>;
export default meta;

type Story = StoryObj<typeof meta>;

function Harness({ initial }: { initial: FilterGroupValue }) {
  const [values, setValues] = useState<FilterGroupValue>(initial);
  function toggle(key: string, value: string) {
    setValues((prev) => {
      const next: Record<string, FilterValue> = { ...prev };
      const v = prev[key];
      if (Array.isArray(v)) {
        const list = [...v];
        const idx = list.indexOf(value);
        if (idx >= 0) list.splice(idx, 1);
        else list.push(value);
        next[key] = list;
      } else {
        next[key] = v === value ? '' : value;
      }
      return next;
    });
  }
  function clear(key?: string) {
    setValues((prev) => {
      if (key === undefined) {
        const n: Record<string, FilterValue> = {};
        for (const d of DEFS) n[d.key] = d.kind === 'multi' ? [] : '';
        return n;
      }
      const def = DEFS.find((d) => d.key === key);
      if (!def) return prev;
      return { ...prev, [key]: def.kind === 'multi' ? [] : '' };
    });
  }
  return <FilterChips definitions={DEFS} values={values} onToggleOption={toggle} onClear={clear} />;
}

export const Default: Story = {
  render: () => <Harness initial={{ status: '', type: [] }} />,
};

export const ActiveSingle: Story = {
  render: () => <Harness initial={{ status: 'ACTIVE', type: [] }} />,
};

export const ActiveMulti: Story = {
  render: () => <Harness initial={{ status: 'ACTIVE', type: ['SUPPLY', 'SERVICE'] }} />,
};

export const AllActive: Story = {
  render: () => (
    <Harness initial={{ status: 'ARCHIVED', type: ['SUPPLY', 'SERVICE', 'LICENSE'] }} />
  ),
};
