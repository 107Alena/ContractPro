import { cva, type VariantProps } from 'class-variance-authority';
import type { SVGAttributes } from 'react';

import { cn } from '@/shared/lib/cn';

const spinnerVariants = cva('animate-spin', {
  variants: {
    size: {
      sm: 'h-3.5 w-3.5',
      md: 'h-4 w-4',
      lg: 'h-5 w-5',
    },
  },
  defaultVariants: { size: 'md' },
});

export interface SpinnerProps
  extends Omit<SVGAttributes<SVGSVGElement>, 'children'>, VariantProps<typeof spinnerVariants> {}

export function Spinner({ className, size, ...rest }: SpinnerProps) {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      className={cn(spinnerVariants({ size }), className)}
      viewBox="0 0 24 24"
      fill="none"
      {...rest}
    >
      <circle cx="12" cy="12" r="10" stroke="currentColor" strokeOpacity="0.25" strokeWidth="3" />
      <path
        d="M22 12a10 10 0 0 1-10 10"
        stroke="currentColor"
        strokeWidth="3"
        strokeLinecap="round"
      />
    </svg>
  );
}

export { spinnerVariants };
