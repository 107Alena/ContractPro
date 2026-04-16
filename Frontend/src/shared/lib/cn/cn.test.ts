import { describe, expect, it } from 'vitest';

import { cn } from './cn';

describe('cn', () => {
  it('joins plain class strings', () => {
    expect(cn('a', 'b')).toBe('a b');
  });

  it('drops falsy values', () => {
    expect(cn('a', false, null, undefined, 'b')).toBe('a b');
  });

  it('merges conflicting tailwind classes (twMerge)', () => {
    expect(cn('px-2', 'px-4')).toBe('px-4');
  });

  it('supports conditional objects (clsx)', () => {
    expect(cn('base', { active: true, hidden: false })).toBe('base active');
  });
});
