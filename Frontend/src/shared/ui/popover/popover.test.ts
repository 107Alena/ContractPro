import { describe, expect, it } from 'vitest';

import { popoverContentVariants } from './popover';

describe('popoverContentVariants', () => {
  it('default size is md → w-72', () => {
    expect(popoverContentVariants({})).toContain('w-72');
  });

  it('size sm → w-56', () => {
    expect(popoverContentVariants({ size: 'sm' })).toContain('w-56');
  });

  it('size auto → w-auto', () => {
    expect(popoverContentVariants({ size: 'auto' })).toContain('w-auto');
  });

  it('has z-popover class', () => {
    expect(popoverContentVariants({})).toContain('z-popover');
  });
});
