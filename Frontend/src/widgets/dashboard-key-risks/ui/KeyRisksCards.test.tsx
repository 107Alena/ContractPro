// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import type { ContractSummary } from '@/entities/contract';

import { KeyRisksCards } from './KeyRisksCards';

afterEach(cleanup);

describe('KeyRisksCards', () => {
  it('пустой список → empty-state с сохранённым текстом', () => {
    render(<KeyRisksCards items={[]} />);
    expect(screen.getByText(/риски появятся после первой проверки/i)).toBeDefined();
  });

  it('error → role=alert', () => {
    render(<KeyRisksCards error={new Error('net')} />);
    expect(screen.getByRole('alert')).toBeDefined();
  });

  it('есть проверки → секция «Ключевые риски» + подсказка (skeleton-структура)', () => {
    const items: ContractSummary[] = [{ contract_id: '1', title: 'A', processing_status: 'READY' }];
    render(<KeyRisksCards items={items} />);
    expect(screen.getByRole('region', { name: 'Ключевые риски' })).toBeDefined();
    expect(screen.getByText(/детальные риски доступны/i)).toBeDefined();
  });
});
