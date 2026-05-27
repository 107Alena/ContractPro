import { describe, expect, it } from 'vitest';

import { buttonVariants } from './button';

describe('buttonVariants', () => {
  it('default variant is primary md (figma-aligned: 38px height, semibold)', () => {
    const cls = buttonVariants({});
    expect(cls).toContain('bg-brand-500');
    expect(cls).toContain('h-[38px]');
    expect(cls).toContain('font-semibold');
  });

  it('danger variant uses danger bg and semibold weight', () => {
    const cls = buttonVariants({ variant: 'danger' });
    expect(cls).toContain('bg-danger');
    expect(cls).toContain('font-semibold');
  });

  it('ghost variant is transparent with medium weight', () => {
    const cls = buttonVariants({ variant: 'ghost' });
    expect(cls).toContain('bg-transparent');
    expect(cls).toContain('font-medium');
  });

  it('secondary variant uses 1.5px border (figma-aligned)', () => {
    const cls = buttonVariants({ variant: 'secondary' });
    expect(cls).toContain('border-[1.5px]');
    expect(cls).toContain('font-medium');
  });

  it('primary in md gets px-6 via compoundVariant (figma CTA padding)', () => {
    expect(buttonVariants({ variant: 'primary', size: 'md' })).toContain('px-6');
  });

  it('secondary in md keeps px-5 (no compoundVariant override)', () => {
    expect(buttonVariants({ variant: 'secondary', size: 'md' })).toContain('px-5');
  });

  it('size sm is h-8 with text-13', () => {
    const cls = buttonVariants({ size: 'sm' });
    expect(cls).toContain('h-8');
    expect(cls).toContain('text-13');
  });

  it('size lg is h-12 with text-16', () => {
    const cls = buttonVariants({ size: 'lg' });
    expect(cls).toContain('h-12');
    expect(cls).toContain('text-16');
  });

  it('fullWidth adds w-full', () => {
    expect(buttonVariants({ fullWidth: true })).toContain('w-full');
  });

  it('disabled opacity class is present by default (applied by disabled:*)', () => {
    expect(buttonVariants({})).toContain('disabled:opacity-50');
  });
});
