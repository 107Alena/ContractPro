// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import type { ComparisonRisksGroups } from '../model/types';
import { RisksGroups } from './risks-groups';

afterEach(cleanup);

const GROUPS: ComparisonRisksGroups = {
  resolved: [{ id: 'r1', title: 'Односторонний выход контрагента', level: 'high' }],
  introduced: [
    { id: 'i1', title: 'Штраф 50% от суммы договора', level: 'high', category: 'Финансовый' },
    { id: 'i2', title: 'Уплата НДС сверх цены', level: 'medium' },
  ],
  unchanged: [{ id: 'u1', title: 'Подсудность Москвы', level: 'low' }],
};

describe('RisksGroups', () => {
  it('рендерит три группы с правильными счётчиками', () => {
    render(<RisksGroups groups={GROUPS} />);
    expect(screen.getByTestId('risks-groups')).toBeTruthy();
    expect(screen.getByTestId('risks-groups-resolved-count').textContent).toBe('(1)');
    expect(screen.getByTestId('risks-groups-introduced-count').textContent).toBe('(2)');
    expect(screen.getByTestId('risks-groups-unchanged-count').textContent).toBe('(1)');
  });

  it('resolved и introduced раскрыты по умолчанию (open), unchanged — нет', () => {
    render(<RisksGroups groups={GROUPS} />);
    const resolved = screen.getByTestId('risks-groups-resolved') as HTMLDetailsElement;
    const introduced = screen.getByTestId('risks-groups-introduced') as HTMLDetailsElement;
    const unchanged = screen.getByTestId('risks-groups-unchanged') as HTMLDetailsElement;
    expect(resolved.open).toBe(true);
    expect(introduced.open).toBe(true);
    expect(unchanged.open).toBe(false);
  });

  it('рендерит item с категорией и level-badge', () => {
    render(<RisksGroups groups={GROUPS} />);
    expect(screen.getByText('Штраф 50% от суммы договора')).toBeTruthy();
    expect(screen.getByText('Финансовый')).toBeTruthy();
  });
});
