import { describe, expect, it } from 'vitest';

import { badgeVariants } from './badge';

describe('badgeVariants', () => {
  it('defaults to neutral / size md (figma-aligned)', () => {
    const cls = badgeVariants({});
    expect(cls).toContain('bg-bg-muted');
    expect(cls).toContain('text-12');
    expect(cls).toContain('px-2.5');
    expect(cls).toContain('font-semibold');
  });

  it('success uses success colour mix + text-success', () => {
    const cls = badgeVariants({ variant: 'success' });
    expect(cls).toContain('text-success');
  });

  it('warning maps to risk-medium text (figma intent)', () => {
    const cls = badgeVariants({ variant: 'warning' });
    expect(cls).toContain('text-risk-medium');
  });

  it('brand uses brand-50 bg', () => {
    expect(badgeVariants({ variant: 'brand' })).toContain('bg-brand-50');
  });

  it('size sm uses text-11 + px-2 (table density)', () => {
    const cls = badgeVariants({ size: 'sm' });
    expect(cls).toContain('text-11');
    expect(cls).toContain('px-2');
    expect(cls).not.toContain('px-2.5');
  });
});
