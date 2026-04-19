import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import { Button } from '@/shared/ui/button';

import type { FilterDefinition, FilterGroupValue, FilterValue } from '../model/types';
import { MoreFiltersModal } from './MoreFiltersModal';

const DEFS: readonly FilterDefinition[] = [
  {
    key: 'type',
    label: 'Тип договора',
    kind: 'multi',
    options: [
      { value: 'SUPPLY', label: 'Поставка' },
      { value: 'SERVICE', label: 'Услуги' },
      { value: 'LICENSE', label: 'Лицензия' },
    ],
  },
  {
    key: 'period',
    label: 'Период',
    kind: 'single',
    options: [
      { value: 'DAY', label: 'Сегодня' },
      { value: 'WEEK', label: 'Неделя' },
      { value: 'MONTH', label: 'Месяц' },
    ],
  },
];

const meta = {
  title: 'Features/Filters/MoreFiltersModal',
  component: MoreFiltersModal,
  parameters: { layout: 'centered' },
  tags: ['autodocs'],
  args: {
    open: false,
    onOpenChange: () => undefined,
    definitions: DEFS,
    values: { type: [], period: '' },
    onToggleOption: () => undefined,
    onClear: () => undefined,
  },
} satisfies Meta<typeof MoreFiltersModal>;
export default meta;

type Story = StoryObj<typeof meta>;

function Harness({ initial }: { initial: FilterGroupValue }) {
  const [open, setOpen] = useState(false);
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
  function clear() {
    const n: Record<string, FilterValue> = {};
    for (const d of DEFS) n[d.key] = d.kind === 'multi' ? [] : '';
    setValues(n);
  }
  return (
    <div>
      <Button onClick={() => setOpen(true)}>Открыть «Ещё фильтры»</Button>
      <MoreFiltersModal
        open={open}
        onOpenChange={setOpen}
        definitions={DEFS}
        values={values}
        onToggleOption={toggle}
        onClear={clear}
      />
    </div>
  );
}

export const Default: Story = {
  render: () => <Harness initial={{ type: [], period: '' }} />,
};

export const WithSelection: Story = {
  render: () => <Harness initial={{ type: ['SUPPLY'], period: 'WEEK' }} />,
};
