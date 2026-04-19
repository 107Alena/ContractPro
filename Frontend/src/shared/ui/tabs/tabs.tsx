import * as TabsPrimitive from '@radix-ui/react-tabs';
import { cva, type VariantProps } from 'class-variance-authority';
import { type ComponentPropsWithoutRef, type ElementRef, forwardRef } from 'react';

import { cn } from '@/shared/lib/cn';

const tabsListVariants = cva('inline-flex items-center gap-1', {
  variants: {
    variant: {
      underline: 'border-b border-border',
      pills: 'rounded-md bg-bg-muted p-1',
    },
    size: {
      sm: 'h-8 text-xs',
      md: 'h-10 text-sm',
    },
    fullWidth: {
      true: 'w-full',
      false: '',
    },
  },
  defaultVariants: { variant: 'underline', size: 'md', fullWidth: false },
});

const tabsTriggerVariants = cva(
  [
    'inline-flex items-center justify-center gap-2 whitespace-nowrap font-medium',
    'transition-colors duration-150',
    'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2',
    'disabled:pointer-events-none disabled:opacity-50',
  ],
  {
    variants: {
      variant: {
        underline: [
          'h-full border-b-2 border-transparent px-3 text-fg-muted',
          'hover:text-fg',
          'data-[state=active]:border-brand-500 data-[state=active]:text-brand-600',
        ],
        pills: [
          'h-full rounded-sm px-3 text-fg-muted',
          'hover:text-fg',
          'data-[state=active]:bg-bg data-[state=active]:text-fg data-[state=active]:shadow-sm',
        ],
      },
      size: {
        sm: 'text-xs',
        md: 'text-sm',
      },
      fullWidth: {
        true: 'flex-1',
        false: '',
      },
    },
    defaultVariants: { variant: 'underline', size: 'md', fullWidth: false },
  },
);

const tabsContentVariants = cva(
  [
    'mt-3 focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2 rounded-md',
    'motion-safe:transition-opacity motion-safe:duration-150',
    'data-[state=inactive]:opacity-0',
  ],
  {
    variants: {},
    defaultVariants: {},
  },
);

export const Tabs = TabsPrimitive.Root;

export interface TabsListProps
  extends
    ComponentPropsWithoutRef<typeof TabsPrimitive.List>,
    VariantProps<typeof tabsListVariants> {}

export const TabsList = forwardRef<ElementRef<typeof TabsPrimitive.List>, TabsListProps>(
  function TabsList({ className, variant, size, fullWidth, ...rest }, ref) {
    return (
      <TabsPrimitive.List
        ref={ref}
        className={cn(tabsListVariants({ variant, size, fullWidth }), className)}
        {...rest}
      />
    );
  },
);

export interface TabsTriggerProps
  extends
    ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>,
    VariantProps<typeof tabsTriggerVariants> {}

export const TabsTrigger = forwardRef<ElementRef<typeof TabsPrimitive.Trigger>, TabsTriggerProps>(
  function TabsTrigger({ className, variant, size, fullWidth, ...rest }, ref) {
    return (
      <TabsPrimitive.Trigger
        ref={ref}
        className={cn(tabsTriggerVariants({ variant, size, fullWidth }), className)}
        {...rest}
      />
    );
  },
);

export type TabsContentProps = ComponentPropsWithoutRef<typeof TabsPrimitive.Content>;

export const TabsContent = forwardRef<ElementRef<typeof TabsPrimitive.Content>, TabsContentProps>(
  function TabsContent({ className, ...rest }, ref) {
    return (
      <TabsPrimitive.Content ref={ref} className={cn(tabsContentVariants(), className)} {...rest} />
    );
  },
);

export { tabsContentVariants, tabsListVariants, tabsTriggerVariants };
