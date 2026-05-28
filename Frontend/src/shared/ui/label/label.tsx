import * as LabelPrimitive from '@radix-ui/react-label';
import { cva, type VariantProps } from 'class-variance-authority';
import { type ComponentPropsWithoutRef, forwardRef } from 'react';

import { cn } from '@/shared/lib/cn';

// Figma-aligned: text-fg-strong (#333340) — выделенный gray для labels,
// между fg и fg-muted. Source: nodes 56:9 (Email label), 56:13 (Password label)
// — Auth Desktop. См. §8.2 high-architecture.md.
const labelVariants = cva(['inline-flex items-center gap-1 font-sans font-medium text-fg-strong'], {
  variants: {
    size: {
      sm: 'text-12 leading-4',
      md: 'text-14 leading-5',
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
