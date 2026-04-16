import { describe, expect, it } from 'vitest';

import { inputVariants } from './input';

describe('inputVariants', () => {
  it('defaults: state=default, size=md', () => {
    const cls = inputVariants({});
    expect(cls).toContain('border-border');
    expect(cls).toContain('h-10');
  });

  it('error state uses danger border', () => {
    expect(inputVariants({ state: 'error' })).toContain('border-danger');
  });

  it('size sm/lg', () => {
    expect(inputVariants({ size: 'sm' })).toContain('h-8');
    expect(inputVariants({ size: 'lg' })).toContain('h-12');
  });
});
