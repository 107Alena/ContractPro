import { describe, expect, it } from 'vitest';

import { toastItemVariants } from './toaster';

describe('toastItemVariants', () => {
  it('default variant is info — нейтральный bg/border/text', () => {
    const cls = toastItemVariants({});
    expect(cls).toContain('border-border');
    expect(cls).toContain('bg-bg-muted');
    expect(cls).toContain('text-fg');
  });

  it('success: tinted bg + green border + colored text (figma)', () => {
    const cls = toastItemVariants({ variant: 'success' });
    expect(cls).toContain('border-success/20');
    expect(cls).toContain('text-success');
  });

  it('error: tinted bg + danger border + colored text (figma)', () => {
    const cls = toastItemVariants({ variant: 'error' });
    expect(cls).toContain('border-danger/20');
    expect(cls).toContain('text-danger');
  });

  it('warning: warning tint, text-fg для контраста (yellow слишком светлый)', () => {
    const cls = toastItemVariants({ variant: 'warning' });
    expect(cls).toContain('border-warning/30');
    expect(cls).toContain('text-fg');
  });

  it('базовая typography: text-13 / px-3.5 py-3 / gap-2.5 (figma 14/12, 10)', () => {
    const cls = toastItemVariants({});
    expect(cls).toContain('text-13');
    expect(cls).toContain('px-3.5');
    expect(cls).toContain('py-3');
    expect(cls).toContain('gap-2.5');
  });
});
