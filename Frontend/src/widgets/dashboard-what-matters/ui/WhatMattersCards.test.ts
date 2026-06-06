import { describe, expect, it } from 'vitest';

import type { ContractSummary } from '@/entities/contract';

import { computeCounters } from './WhatMattersCards';

const s = (status: NonNullable<ContractSummary['processing_status']>): ContractSummary => ({
  contract_id: Math.random().toString(),
  title: status,
  status: 'ACTIVE',
  processing_status: status,
  created_at: '2026-04-15T10:00:00Z',
  updated_at: '2026-04-15T10:00:00Z',
});

describe('computeCounters', () => {
  it('returns zeros for empty list but uses server total', () => {
    expect(computeCounters([], 42)).toEqual({
      total: 42,
      inProgress: 0,
      ready: 0,
      failed: 0,
    });
  });

  it('counts in-progress (including awaiting), ready, and failed buckets', () => {
    const items = [
      s('READY'),
      s('READY'),
      s('ANALYZING'),
      s('AWAITING_USER_INPUT'),
      s('FAILED'),
      s('REJECTED'),
      s('UPLOADED'),
    ];
    const counters = computeCounters(items, items.length);
    expect(counters).toEqual({
      total: 7,
      inProgress: 2, // ANALYZING + AWAITING
      ready: 2,
      failed: 2, // FAILED + REJECTED
    });
  });
});
