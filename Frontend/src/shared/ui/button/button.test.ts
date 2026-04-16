import { describe, expect, it } from 'vitest';

import { buttonVariants } from './button';

describe('buttonVariants', () => {
  it('default variant is primary md', () => {
    const cls = buttonVariants({});
    expect(cls).toContain('bg-brand-500');
    expect(cls).toContain('h-10');
  });

  it('danger variant uses danger bg', () => {
    expect(buttonVariants({ variant: 'danger' })).toContain('bg-danger');
  });

  it('ghost variant is transparent', () => {
    expect(buttonVariants({ variant: 'ghost' })).toContain('bg-transparent');
  });

  it('size sm is h-8', () => {
    expect(buttonVariants({ size: 'sm' })).toContain('h-8');
  });

  it('size lg is h-12', () => {
    expect(buttonVariants({ size: 'lg' })).toContain('h-12');
  });

  it('fullWidth adds w-full', () => {
    expect(buttonVariants({ fullWidth: true })).toContain('w-full');
  });

  it('disabled opacity class is present by default (applied by disabled:*)', () => {
    expect(buttonVariants({})).toContain('disabled:opacity-50');
  });
});
