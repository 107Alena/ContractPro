// @vitest-environment jsdom
import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import type { ContractSummary } from '@/entities/contract';

import { ProcessingStatus } from './ProcessingStatus';

afterEach(cleanup);

describe('ProcessingStatus', () => {
  it('активная проверка (ANALYZING) → шаги + название, 2 завершённых', () => {
    const items: ContractSummary[] = [
      { contract_id: 'c1', title: 'Аренда', processing_status: 'ANALYZING' },
    ];
    render(<ProcessingStatus items={items} />);
    const region = screen.getByRole('article', { name: 'Статус обработки' });
    expect(within(region).getByText('Юр. анализ')).toBeDefined();
    expect(within(region).getByText('Аренда')).toBeDefined();
    // honesty: Загружен + Извлечение текста done (✓✓), Юр. анализ — активный
    expect(within(region).getAllByText('✓')).toHaveLength(2);
  });

  it('PROCESSING не завышает прогресс — done только «Загружен»', () => {
    const items: ContractSummary[] = [
      { contract_id: 'c1', title: 'X', processing_status: 'PROCESSING' },
    ];
    render(<ProcessingStatus items={items} />);
    const region = screen.getByRole('article', { name: 'Статус обработки' });
    expect(within(region).getAllByText('✓')).toHaveLength(1);
    expect(within(region).getByText('Извлечение текста')).toBeDefined();
  });

  it('нет активных → empty-state', () => {
    const items: ContractSummary[] = [
      { contract_id: 'c1', title: 'Готов', processing_status: 'READY' },
    ];
    render(<ProcessingStatus items={items} />);
    expect(screen.getByText(/нет активных проверок/i)).toBeDefined();
  });
});
