import { describe, expect, it } from 'vitest';

import { modalContentVariants } from './modal';

describe('modalContentVariants', () => {
  it('default size is md', () => {
    expect(modalContentVariants({})).toContain('sm:max-w-md');
  });

  it('size sm maps to max-w-sm', () => {
    expect(modalContentVariants({ size: 'sm' })).toContain('sm:max-w-sm');
  });

  it('size lg maps to max-w-2xl', () => {
    expect(modalContentVariants({ size: 'lg' })).toContain('sm:max-w-2xl');
  });

  it('always pins to z-modal', () => {
    expect(modalContentVariants({})).toContain('z-modal');
  });
});
