import { cva, type VariantProps } from 'class-variance-authority';
import { forwardRef, type HTMLAttributes } from 'react';

import { cn } from '@/shared/lib/cn';

// Figma-aligned: nodes 89:7 RiskBadge/Medium, 92:7 LevelBadge, 253:6
// StatusBadge/Warning. Badges используют semantic background-tint (alpha 10-15%
// от соответствующего color) + насыщенный text-цвет. Размеры: sm (table density,
// text-11) / md (cards, text-12). См. §8.2 + §8.3 high-architecture.md.
const badgeVariants = cva(
  ['inline-flex items-center gap-1 whitespace-nowrap rounded-sm font-semibold leading-4'],
  {
    variants: {
      variant: {
        success: 'bg-[color-mix(in_srgb,var(--color-success)_14%,transparent)] text-success',
        warning: 'bg-[color-mix(in_srgb,var(--color-warning)_15%,transparent)] text-risk-medium',
        danger: 'bg-[color-mix(in_srgb,var(--color-danger)_10%,transparent)] text-danger',
        neutral: 'bg-bg-muted text-fg-muted',
        brand: 'bg-brand-50 text-brand-600',
      },
      size: {
        sm: 'px-2 py-1 text-11',
        md: 'px-2.5 py-1 text-12',
      },
    },
    defaultVariants: { variant: 'neutral', size: 'md' },
  },
);

export interface BadgeProps
  extends HTMLAttributes<HTMLSpanElement>, VariantProps<typeof badgeVariants> {}

export const Badge = forwardRef<HTMLSpanElement, BadgeProps>(function Badge(
  { className, variant, size, ...rest },
  ref,
) {
  return <span ref={ref} className={cn(badgeVariants({ variant, size }), className)} {...rest} />;
});

export { badgeVariants };
