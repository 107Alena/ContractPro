// Stories для всех 8 виджетов сравнения версий. Один meta-объект (Overview),
// в котором каждая story рендерит один из под-компонентов с реалистичными
// данными (русские договорные формулировки, разделы вида «section.X»).
import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import type {
  VersionDiffStructuralChange,
  VersionDiffTextChange,
} from '@/features/comparison-start';

import type { ChangesFilter, ComparisonRisksGroups, RiskProfileSnapshot } from '../model/types';
import { ChangeCounters } from './change-counters';
import { ChangesTable } from './changes-table';
import { ComparisonVerdictCard } from './comparison-verdict-card';
import { KeyDiffsBySection } from './key-diffs-by-section';
import { RiskProfileDelta } from './risk-profile-delta';
import { RisksGroups } from './risks-groups';
import { TabsFilters } from './tabs-filters';
import { VersionMetaHeader } from './version-meta-header';

const SAMPLE_TEXT_CHANGES: readonly VersionDiffTextChange[] = [
  {
    type: 'added',
    path: 'section.2/clause.1',
    new_text:
      'Покупатель производит предоплату в размере 30% от цены товара в течение 5 (пяти) рабочих дней с момента подписания Договора.',
  },
  {
    type: 'removed',
    path: 'section.4/clause.2',
    old_text:
      'За нарушение сроков поставки Поставщик уплачивает неустойку в размере 0,1% от стоимости непоставленного товара за каждый день просрочки.',
  },
  {
    type: 'modified',
    path: 'section.3/clause.1',
    old_text: 'Срок поставки — 30 календарных дней.',
    new_text: 'Срок поставки — 30 рабочих дней с момента поступления предоплаты.',
  },
  {
    type: 'modified',
    path: 'section.2/clause.3',
    old_text: 'Цена товара 1 200 000 рублей.',
    new_text: 'Цена товара 1 350 000 рублей, в т.ч. НДС 20%.',
  },
];

const SAMPLE_STRUCTURAL_CHANGES: readonly VersionDiffStructuralChange[] = [
  { type: 'moved', node_id: 'section.5' },
  { type: 'added', node_id: 'section.7' },
];

const BASE_PROFILE: RiskProfileSnapshot = { high: 4, medium: 3, low: 2 };
const TARGET_PROFILE: RiskProfileSnapshot = { high: 2, medium: 4, low: 2 };

const RISKS_GROUPS: ComparisonRisksGroups = {
  resolved: [
    {
      id: 'r1',
      title: 'Право одностороннего отказа Покупателя без компенсации',
      level: 'high',
      category: 'Юридический',
    },
    {
      id: 'r2',
      title: 'Отсутствие пункта о форс-мажоре',
      level: 'medium',
    },
  ],
  introduced: [
    {
      id: 'i1',
      title: 'Штраф 50% от суммы договора при просрочке оплаты',
      level: 'high',
      category: 'Финансовый',
    },
    {
      id: 'i2',
      title: 'НДС начисляется сверх цены товара',
      level: 'medium',
      category: 'Налоговый',
    },
  ],
  unchanged: [
    {
      id: 'u1',
      title: 'Подсудность — Арбитражный суд г. Москвы',
      level: 'low',
    },
    {
      id: 'u2',
      title: 'Срок действия — 1 год с автоматической пролонгацией',
      level: 'low',
    },
  ],
};

const meta = {
  title: 'Widgets/VersionCompare/Overview',
  parameters: { layout: 'padded' },
  tags: ['autodocs'],
  decorators: [
    (Story) => (
      <div className="max-w-5xl">
        <Story />
      </div>
    ),
  ],
} satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;

export const VersionMetaHeaderDefault: Story = {
  render: () => (
    <VersionMetaHeader
      base={{
        versionId: 'v-base',
        versionNumber: 1,
        title: 'Договор поставки оборудования №42/2026',
        authorName: 'Иванов И.И.',
        createdAt: '2026-01-15T09:00:00Z',
      }}
      target={{
        versionId: 'v-target',
        versionNumber: 2,
        title: 'Договор поставки оборудования №42/2026 (ред. 2)',
        authorName: 'Петров П.П.',
        createdAt: '2026-04-19T12:30:00Z',
      }}
    />
  ),
};

export const ComparisonVerdictBetter: Story = {
  render: () => (
    <ComparisonVerdictCard
      verdict="better"
      baseProfile={BASE_PROFILE}
      targetProfile={TARGET_PROFILE}
    />
  ),
};

export const ChangeCountersDefault: Story = {
  render: () => (
    <ChangeCounters
      counters={{
        total: 6,
        added: 2,
        removed: 1,
        modified: 2,
        moved: 1,
        textual: 4,
        structural: 2,
      }}
    />
  ),
};

function TabsFiltersInteractive(): JSX.Element {
  const [value, setValue] = useState<ChangesFilter>('all');
  return (
    <TabsFilters
      value={value}
      onChange={setValue}
      counters={{ all: 6, textual: 4, structural: 2, 'high-risk': 1 }}
    />
  );
}

export const TabsFiltersDefault: Story = {
  render: () => <TabsFiltersInteractive />,
};

export const ChangesTableDefault: Story = {
  render: () => (
    <ChangesTable changes={SAMPLE_TEXT_CHANGES} structuralChanges={SAMPLE_STRUCTURAL_CHANGES} />
  ),
};

export const RiskProfileDeltaDefault: Story = {
  render: () => (
    <RiskProfileDelta
      delta={{ high: -2, medium: 1, low: 0 }}
      baseProfile={BASE_PROFILE}
      targetProfile={TARGET_PROFILE}
    />
  ),
};

export const KeyDiffsBySectionDefault: Story = {
  render: () => (
    <KeyDiffsBySection
      sections={[
        { section: 'section.2', added: 1, removed: 0, modified: 1 },
        { section: 'section.3', added: 0, removed: 0, modified: 1 },
        { section: 'section.4', added: 0, removed: 1, modified: 0 },
        { section: 'section.5', added: 0, removed: 0, modified: 1 },
        { section: 'section.7', added: 1, removed: 0, modified: 0 },
      ]}
    />
  ),
};

export const RisksGroupsDefault: Story = {
  render: () => <RisksGroups groups={RISKS_GROUPS} />,
};
