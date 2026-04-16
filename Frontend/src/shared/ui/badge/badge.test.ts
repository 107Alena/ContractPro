import { describe, expect, it } from 'vitest';

import { badgeVariants } from './badge';

describe('badgeVariants', () => {
  it('defaults to neutral', () => {
    expect(badgeVariants({})).toContain('bg-bg-muted');
  });

  it('success uses success colour mix', () => {
    const cls = badgeVariants({ variant: 'success' });
    expect(cls).toContain('text-success');
  });

  it('brand uses brand-50 bg', () => {
    expect(badgeVariants({ variant: 'brand' })).toContain('bg-brand-50');
  });
});
