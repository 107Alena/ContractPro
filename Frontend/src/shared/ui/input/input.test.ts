import { describe, expect, it } from 'vitest';

import { inputVariants } from './input';

describe('inputVariants', () => {
  it('defaults: state=default, size=md (figma-aligned px-4, text-15)', () => {
    const cls = inputVariants({});
    expect(cls).toContain('border-border');
    expect(cls).toContain('h-10');
    expect(cls).toContain('px-4');
    expect(cls).toContain('text-15');
    expect(cls).toContain('placeholder:text-fg-disabled');
  });

  it('error state: 1.5px border-danger + light danger-bg tint', () => {
    const cls = inputVariants({ state: 'error' });
    expect(cls).toContain('border-danger');
    expect(cls).toContain('border-[1.5px]');
    expect(cls).toContain('bg-danger-bg');
  });

  it('disabled styles: bg-muted + fg-subtle + opacity-70', () => {
    const cls = inputVariants({});
    expect(cls).toContain('disabled:bg-bg-muted');
    expect(cls).toContain('disabled:text-fg-subtle');
    expect(cls).toContain('disabled:opacity-70');
  });

  it('size sm/lg with token-based text sizes', () => {
    const sm = inputVariants({ size: 'sm' });
    expect(sm).toContain('h-8');
    expect(sm).toContain('text-13');
    const lg = inputVariants({ size: 'lg' });
    expect(lg).toContain('h-12');
    expect(lg).toContain('text-16');
  });
});
