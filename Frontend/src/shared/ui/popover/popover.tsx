import * as PopoverPrimitive from '@radix-ui/react-popover';
import { cva, type VariantProps } from 'class-variance-authority';
import { type ComponentPropsWithoutRef, type ElementRef, forwardRef } from 'react';

import { cn } from '@/shared/lib/cn';

const contentVariants = cva(
  [
    'z-popover rounded-md border border-border bg-bg p-3 text-sm text-fg shadow-md outline-none',
    'focus-visible:ring focus-visible:ring-offset-0',
    'motion-safe:transition motion-safe:duration-100',
    'data-[state=closed]:motion-safe:opacity-0',
  ],
  {
    variants: {
      size: {
        sm: 'w-56',
        md: 'w-72',
        lg: 'w-96',
        auto: 'w-auto',
      },
    },
    defaultVariants: { size: 'md' },
  },
);

export const Popover = PopoverPrimitive.Root;
export const PopoverTrigger = PopoverPrimitive.Trigger;
export const PopoverAnchor = PopoverPrimitive.Anchor;
export const PopoverPortal = PopoverPrimitive.Portal;
export const PopoverClose = PopoverPrimitive.Close;
export const PopoverArrow = PopoverPrimitive.Arrow;

export interface PopoverContentProps
  extends
    ComponentPropsWithoutRef<typeof PopoverPrimitive.Content>,
    VariantProps<typeof contentVariants> {}

export const PopoverContent = forwardRef<
  ElementRef<typeof PopoverPrimitive.Content>,
  PopoverContentProps
>(function PopoverContent({ className, size, sideOffset = 6, align = 'start', ...rest }, ref) {
  return (
    <PopoverPortal>
      <PopoverPrimitive.Content
        ref={ref}
        sideOffset={sideOffset}
        align={align}
        className={cn(contentVariants({ size }), className)}
        {...rest}
      />
    </PopoverPortal>
  );
});

export { contentVariants as popoverContentVariants };
