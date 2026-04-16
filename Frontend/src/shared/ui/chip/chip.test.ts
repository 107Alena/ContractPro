import { describe, expect, it } from 'vitest';

import { chipVariants } from './chip';

describe('chipVariants', () => {
  it('defaults: not selected, not interactive', () => {
    const cls = chipVariants({});
    expect(cls).toContain('border-border');
    expect(cls).not.toContain('cursor-pointer');
  });

  it('selected switches to brand palette', () => {
    const cls = chipVariants({ selected: true });
    expect(cls).toContain('border-brand-500');
    expect(cls).toContain('bg-brand-50');
  });

  it('interactive adds focus-visible ring and pointer', () => {
    const cls = chipVariants({ interactive: true });
    expect(cls).toContain('cursor-pointer');
    expect(cls).toContain('focus-visible:ring');
  });
});
