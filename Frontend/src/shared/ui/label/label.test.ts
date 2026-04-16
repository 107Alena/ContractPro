import { describe, expect, it } from 'vitest';

import { labelVariants } from './label';

describe('labelVariants', () => {
  it('default md size -> text-sm', () => {
    expect(labelVariants({})).toContain('text-sm');
  });

  it('sm size -> text-xs', () => {
    expect(labelVariants({ size: 'sm' })).toContain('text-xs');
  });
});
