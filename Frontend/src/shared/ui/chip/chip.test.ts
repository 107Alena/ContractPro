import { describe, expect, it } from 'vitest';

import { chipVariants } from './chip';

describe('chipVariants', () => {
  it('defaults (figma-aligned): pill, white bg, subtle border, text-13', () => {
    const cls = chipVariants({});
    expect(cls).toContain('rounded-pill');
    expect(cls).toContain('border-border-subtle');
    expect(cls).toContain('bg-bg');
    expect(cls).toContain('text-fg-muted');
    expect(cls).toContain('text-13');
    expect(cls).not.toContain('cursor-pointer');
  });

  it('selected switches to solid brand-500 + white text (figma)', () => {
    const cls = chipVariants({ selected: true });
    expect(cls).toContain('bg-brand-500');
    expect(cls).toContain('text-white');
    expect(cls).toContain('border-transparent');
  });

  it('interactive adds focus-visible ring and pointer', () => {
    const cls = chipVariants({ interactive: true });
    expect(cls).toContain('cursor-pointer');
    expect(cls).toContain('focus-visible:ring');
  });

  it('compoundVariant interactive+selected adds darker brand hover', () => {
    expect(chipVariants({ interactive: true, selected: true })).toContain('hover:bg-brand-600');
  });

  it('compoundVariant interactive+unselected adds brand border hover', () => {
    expect(chipVariants({ interactive: true, selected: false })).toContain(
      'hover:border-brand-500',
    );
  });
});
