import { describe, expect, it } from 'vitest';

import type { ContractSummary } from '@/entities/contract';

import { splitByBucket } from './KeyRisksCards';

const s = (status: NonNullable<ContractSummary['processing_status']>): ContractSummary => ({
  contract_id: Math.random().toString(),
  processing_status: status,
});

describe('splitByBucket', () => {
  it('groups READY contracts into ready bucket', () => {
    const result = splitByBucket([s('READY'), s('READY'), s('PROCESSING')]);
    expect(result.ready).toHaveLength(2);
    expect(result.awaiting).toHaveLength(0);
    expect(result.failed).toHaveLength(0);
  });

  it('groups AWAITING_USER_INPUT into awaiting bucket', () => {
    const result = splitByBucket([s('AWAITING_USER_INPUT')]);
    expect(result.awaiting).toHaveLength(1);
  });

  it('groups FAILED / REJECTED / PARTIALLY_FAILED into failed bucket', () => {
    const result = splitByBucket([s('FAILED'), s('REJECTED'), s('PARTIALLY_FAILED')]);
    expect(result.failed).toHaveLength(3);
  });

  it('ignores in-progress statuses (they are not surfaced as risks here)', () => {
    const result = splitByBucket([s('PROCESSING'), s('ANALYZING'), s('QUEUED')]);
    expect(result.ready).toHaveLength(0);
    expect(result.awaiting).toHaveLength(0);
    expect(result.failed).toHaveLength(0);
  });
});
