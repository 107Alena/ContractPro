import { cva, type VariantProps } from 'class-variance-authority';
import { forwardRef, type HTMLAttributes } from 'react';

import { cn } from '@/shared/lib/cn';

const badgeVariants = cva(
  [
    'inline-flex items-center gap-1 whitespace-nowrap',
    'rounded-sm px-2 py-0.5',
    'text-xs font-medium leading-4',
  ],
  {
    variants: {
      variant: {
        success: 'bg-[color-mix(in_srgb,var(--color-success)_14%,transparent)] text-success',
        warning: 'bg-[color-mix(in_srgb,var(--color-warning)_24%,transparent)] text-fg',
        danger: 'bg-[color-mix(in_srgb,var(--color-danger)_14%,transparent)] text-danger',
        neutral: 'bg-bg-muted text-fg-muted',
        brand: 'bg-brand-50 text-brand-600',
      },
    },
    defaultVariants: { variant: 'neutral' },
  },
);

export interface BadgeProps
  extends HTMLAttributes<HTMLSpanElement>, VariantProps<typeof badgeVariants> {}

export const Badge = forwardRef<HTMLSpanElement, BadgeProps>(function Badge(
  { className, variant, ...rest },
  ref,
) {
  return <span ref={ref} className={cn(badgeVariants({ variant }), className)} {...rest} />;
});

export { badgeVariants };
