import { describe, expect, it } from 'vitest';

import type { ContractSummary } from '@/entities/contract';

import { selectActionItems } from './CurrentActions';

const s = (status: NonNullable<ContractSummary['processing_status']>): ContractSummary => ({
  contract_id: status,
  title: status,
  status: 'ACTIVE',
  processing_status: status,
  created_at: '2026-04-15T10:00:00Z',
  updated_at: '2026-04-15T10:00:00Z',
});

describe('selectActionItems', () => {
  it('размечает in_progress/awaiting и пропускает ready/pending', () => {
    const result = selectActionItems([
      s('READY'),
      s('ANALYZING'),
      s('AWAITING_USER_INPUT'),
      s('UPLOADED'),
    ]);
    expect(result.map((a) => a.kind)).toEqual(['processing', 'awaiting']);
  });

  it('maps PROCESSING→processing, AWAITING→awaiting, FAILED→failed', () => {
    const result = selectActionItems([s('PROCESSING'), s('AWAITING_USER_INPUT'), s('FAILED')]);
    expect(result.map((a) => a.kind)).toEqual(['processing', 'awaiting', 'failed']);
  });

  it('ограничивает выборку лимитом (по умолчанию 3)', () => {
    const result = selectActionItems([s('ANALYZING'), s('PROCESSING'), s('FAILED'), s('REJECTED')]);
    expect(result).toHaveLength(3);
  });

  it('возвращает пустой массив, если нет требующих внимания', () => {
    expect(selectActionItems([s('READY'), s('UPLOADED')])).toEqual([]);
  });
});
