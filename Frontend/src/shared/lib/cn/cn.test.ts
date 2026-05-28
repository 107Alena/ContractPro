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

  // Регрессия Этапа 3.1: без extendTailwindMerge `text-15` (custom fontSize)
  // классифицировался как color-утилита, перетирал `text-white` → primary-кнопки
  // получали чёрный текст на оранжевом фоне. См. cn.ts — расширение classGroups.
  it('keeps text color when combined with custom numeric font-size', () => {
    expect(cn('text-white', 'text-15')).toBe('text-white text-15');
    expect(cn('text-fg', 'text-13')).toBe('text-fg text-13');
  });

  it('merges conflicting custom font sizes', () => {
    expect(cn('text-13', 'text-15')).toBe('text-15');
  });

  it('merges conflicting custom shadows', () => {
    expect(cn('shadow-sm', 'shadow-card')).toBe('shadow-card');
  });

  it('merges conflicting custom border radii', () => {
    expect(cn('rounded-md', 'rounded-pill')).toBe('rounded-pill');
  });
});
