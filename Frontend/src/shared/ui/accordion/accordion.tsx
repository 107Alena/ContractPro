import * as AccordionPrimitive from '@radix-ui/react-accordion';
import { cva, type VariantProps } from 'class-variance-authority';
import { type ComponentPropsWithoutRef, type ElementRef, forwardRef } from 'react';

import { cn } from '@/shared/lib/cn';

const accordionItemVariants = cva('', {
  variants: {
    variant: {
      bordered: 'border-b border-border',
      ghost: '',
    },
  },
  defaultVariants: { variant: 'bordered' },
});

const accordionTriggerVariants = cva(
  [
    'flex w-full items-center justify-between gap-2 py-3 text-left text-sm font-medium text-fg',
    'transition-colors',
    'hover:text-brand-600',
    'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2',
    'disabled:pointer-events-none disabled:opacity-50',
    '[&>svg]:shrink-0 [&>svg]:transition-transform [&>svg]:duration-200',
    '[&[data-state=open]>svg]:rotate-180',
  ],
  {
    variants: {
      size: {
        sm: 'py-2 text-xs',
        md: 'py-3 text-sm',
      },
    },
    defaultVariants: { size: 'md' },
  },
);

const accordionContentVariants = cva(
  [
    'overflow-hidden text-sm text-fg-muted',
    'motion-safe:data-[state=closed]:animate-accordion-up',
    'motion-safe:data-[state=open]:animate-accordion-down',
  ],
  {
    variants: {},
    defaultVariants: {},
  },
);

function ChevronDownIcon(): JSX.Element {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      viewBox="0 0 16 16"
      width="16"
      height="16"
      fill="none"
      className="text-fg-muted"
    >
      <path
        d="m4 6 4 4 4-4"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

export const Accordion = AccordionPrimitive.Root;

export interface AccordionItemProps
  extends
    ComponentPropsWithoutRef<typeof AccordionPrimitive.Item>,
    VariantProps<typeof accordionItemVariants> {}

export const AccordionItem = forwardRef<
  ElementRef<typeof AccordionPrimitive.Item>,
  AccordionItemProps
>(function AccordionItem({ className, variant, ...rest }, ref) {
  return (
    <AccordionPrimitive.Item
      ref={ref}
      className={cn(accordionItemVariants({ variant }), className)}
      {...rest}
    />
  );
});

export interface AccordionTriggerProps
  extends
    ComponentPropsWithoutRef<typeof AccordionPrimitive.Trigger>,
    VariantProps<typeof accordionTriggerVariants> {
  hideChevron?: boolean;
}

export const AccordionTrigger = forwardRef<
  ElementRef<typeof AccordionPrimitive.Trigger>,
  AccordionTriggerProps
>(function AccordionTrigger({ className, children, size, hideChevron = false, ...rest }, ref) {
  return (
    <AccordionPrimitive.Header className="flex">
      <AccordionPrimitive.Trigger
        ref={ref}
        className={cn(accordionTriggerVariants({ size }), className)}
        {...rest}
      >
        {children}
        {hideChevron ? null : <ChevronDownIcon />}
      </AccordionPrimitive.Trigger>
    </AccordionPrimitive.Header>
  );
});

export type AccordionContentProps = ComponentPropsWithoutRef<typeof AccordionPrimitive.Content>;

export const AccordionContent = forwardRef<
  ElementRef<typeof AccordionPrimitive.Content>,
  AccordionContentProps
>(function AccordionContent({ className, children, ...rest }, ref) {
  return (
    <AccordionPrimitive.Content
      ref={ref}
      className={cn(accordionContentVariants(), className)}
      {...rest}
    >
      <div className="pb-3 pt-0">{children}</div>
    </AccordionPrimitive.Content>
  );
});

export { accordionContentVariants, accordionItemVariants, accordionTriggerVariants };
