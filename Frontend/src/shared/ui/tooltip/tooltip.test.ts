import { describe, expect, it } from 'vitest';

import { tooltipContentVariants } from './tooltip';

describe('tooltipContentVariants', () => {
  it('default size is md → max-w-[320px]', () => {
    expect(tooltipContentVariants({})).toContain('max-w-[320px]');
  });

  it('size sm caps to 220px', () => {
    expect(tooltipContentVariants({ size: 'sm' })).toContain('max-w-[220px]');
  });

  it('has z-tooltip class', () => {
    expect(tooltipContentVariants({})).toContain('z-tooltip');
  });
});
