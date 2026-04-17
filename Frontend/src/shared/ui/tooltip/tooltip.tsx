import * as TooltipPrimitive from '@radix-ui/react-tooltip';
import { cva, type VariantProps } from 'class-variance-authority';
import { type ComponentPropsWithoutRef, type ElementRef, forwardRef, type ReactNode } from 'react';

import { cn } from '@/shared/lib/cn';

const contentVariants = cva(
  [
    'z-tooltip overflow-hidden rounded-md bg-fg px-2.5 py-1.5 text-xs text-white shadow-md',
    'max-w-[var(--tooltip-max-width)]',
    'motion-safe:transition motion-safe:duration-100',
    'data-[state=closed]:motion-safe:opacity-0',
  ],
  {
    variants: {
      size: {
        sm: 'max-w-[220px]',
        md: 'max-w-[320px]',
      },
    },
    defaultVariants: { size: 'md' },
  },
);

/**
 * Global TooltipProvider. Монтируется один раз в `app/providers`. Внутри —
 * tooltip'ы используют общий `delayDuration`. §8.3: 500 ms по умолчанию.
 */
export const TooltipProvider = TooltipPrimitive.Provider;
export const Tooltip = TooltipPrimitive.Root;
export const TooltipTrigger = TooltipPrimitive.Trigger;
export const TooltipPortal = TooltipPrimitive.Portal;
export const TooltipArrow = TooltipPrimitive.Arrow;

export interface TooltipContentProps
  extends
    ComponentPropsWithoutRef<typeof TooltipPrimitive.Content>,
    VariantProps<typeof contentVariants> {}

export const TooltipContent = forwardRef<
  ElementRef<typeof TooltipPrimitive.Content>,
  TooltipContentProps
>(function TooltipContent({ className, size, sideOffset = 6, ...rest }, ref) {
  return (
    <TooltipPortal>
      <TooltipPrimitive.Content
        ref={ref}
        sideOffset={sideOffset}
        className={cn(contentVariants({ size }), className)}
        {...rest}
      />
    </TooltipPortal>
  );
});

export interface SimpleTooltipProps extends Pick<
  TooltipPrimitive.TooltipProps,
  'defaultOpen' | 'open' | 'onOpenChange'
> {
  content: ReactNode;
  children: ReactNode;
  side?: TooltipPrimitive.TooltipContentProps['side'];
  align?: TooltipPrimitive.TooltipContentProps['align'];
  size?: VariantProps<typeof contentVariants>['size'];
  delayDuration?: number;
  disableHoverableContent?: boolean;
  /** Если TooltipProvider не поднят выше — пробросить локальный. */
  withLocalProvider?: boolean;
}

/**
 * Удобная обёртка для самого частого кейса: один trigger + один content.
 * `withLocalProvider` — для Storybook или одиночных страниц без глобального
 * провайдера (ADR-FE-20).
 */
export function SimpleTooltip({
  content,
  children,
  side = 'top',
  align = 'center',
  size,
  defaultOpen,
  open,
  onOpenChange,
  delayDuration = 500,
  disableHoverableContent,
  withLocalProvider = false,
}: SimpleTooltipProps) {
  const rootProps: TooltipPrimitive.TooltipProps = { delayDuration };
  if (defaultOpen !== undefined) rootProps.defaultOpen = defaultOpen;
  if (open !== undefined) rootProps.open = open;
  if (onOpenChange !== undefined) rootProps.onOpenChange = onOpenChange;
  if (disableHoverableContent !== undefined) {
    rootProps.disableHoverableContent = disableHoverableContent;
  }
  const root = (
    <Tooltip {...rootProps}>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent size={size} side={side} align={align}>
        {content}
      </TooltipContent>
    </Tooltip>
  );
  if (!withLocalProvider) return root;
  return <TooltipProvider delayDuration={delayDuration}>{root}</TooltipProvider>;
}

export { contentVariants as tooltipContentVariants };
