// @vitest-environment jsdom
import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import type { ContractSummary } from '@/entities/contract';

import { ProcessingStatus } from './ProcessingStatus';

afterEach(cleanup);

describe('ProcessingStatus', () => {
  it('активная проверка → шаги + название', () => {
    const items: ContractSummary[] = [
      { contract_id: 'c1', title: 'Аренда', processing_status: 'ANALYZING' },
    ];
    render(<ProcessingStatus items={items} />);
    const region = screen.getByRole('region', { name: 'Статус обработки' });
    expect(within(region).getByText('Анализ рисков')).toBeDefined();
    expect(within(region).getByText('Аренда')).toBeDefined();
  });

  it('нет активных → empty-state', () => {
    const items: ContractSummary[] = [
      { contract_id: 'c1', title: 'Готов', processing_status: 'READY' },
    ];
    render(<ProcessingStatus items={items} />);
    expect(screen.getByText(/нет активных проверок/i)).toBeDefined();
  });
});
