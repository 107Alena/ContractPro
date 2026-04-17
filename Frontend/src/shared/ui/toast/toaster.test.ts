import { describe, expect, it } from 'vitest';

import { toastItemVariants } from './toaster';

describe('toastItemVariants', () => {
  it('default variant is info', () => {
    expect(toastItemVariants({})).toContain('border-border');
  });

  it('success uses success border tint', () => {
    expect(toastItemVariants({ variant: 'success' })).toContain('border-success/40');
  });

  it('error uses danger border tint', () => {
    expect(toastItemVariants({ variant: 'error' })).toContain('border-danger/40');
  });

  it('warning uses warning border tint', () => {
    expect(toastItemVariants({ variant: 'warning' })).toContain('border-warning/40');
  });
});
