import { describe, expect, it } from 'vitest';

import { labelVariants } from './label';

describe('labelVariants', () => {
  it('default md size: text-14 + figma-aligned fg-strong color', () => {
    const cls = labelVariants({});
    expect(cls).toContain('text-14');
    expect(cls).toContain('text-fg-strong');
    expect(cls).toContain('font-medium');
  });

  it('sm size: text-12', () => {
    expect(labelVariants({ size: 'sm' })).toContain('text-12');
  });
});
