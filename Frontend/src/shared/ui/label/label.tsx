import * as LabelPrimitive from '@radix-ui/react-label';
import { cva, type VariantProps } from 'class-variance-authority';
import { type ComponentPropsWithoutRef, forwardRef } from 'react';

import { cn } from '@/shared/lib/cn';

const labelVariants = cva(['inline-flex items-center gap-1 font-sans font-medium text-fg'], {
  variants: {
    size: {
      sm: 'text-xs leading-4',
      md: 'text-sm leading-5',
    },
  },
  defaultVariants: { size: 'md' },
});

type LabelVariantProps = VariantProps<typeof labelVariants>;

export interface LabelProps
  extends ComponentPropsWithoutRef<typeof LabelPrimitive.Root>, LabelVariantProps {
  required?: boolean;
}

export const Label = forwardRef<HTMLLabelElement, LabelProps>(function Label(
  { className, size, required = false, children, ...rest },
  ref,
) {
  return (
    <LabelPrimitive.Root ref={ref} className={cn(labelVariants({ size }), className)} {...rest}>
      {children}
      {required ? (
        <span aria-hidden="true" className="text-danger">
          *
        </span>
      ) : null}
    </LabelPrimitive.Root>
  );
});

export { labelVariants };
