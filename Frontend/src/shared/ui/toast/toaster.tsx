import * as Toast from '@radix-ui/react-toast';
import { cva, type VariantProps } from 'class-variance-authority';
import {
  type ComponentPropsWithoutRef,
  type ElementRef,
  forwardRef,
  type HTMLAttributes,
  useEffect,
} from 'react';

import { cn } from '@/shared/lib/cn';

import { type ToastRecord, type ToastVariant, useToastStore } from './toast-store';

// Figma-aligned: node 58:7 (ErrorToast) — Auth Desktop. Error использует
// tinted bg (danger @ ~8%) + colored text вместо нашего белого bg + colored
// border. Применяем pattern симметрично ко всем семантическим вариантам:
// success/error/warning имеют semantic-tinted bg + colored text;
// info/sticky остаются нейтральными.
const itemVariants = cva(
  [
    'pointer-events-auto flex w-full gap-2.5 rounded-md border px-3.5 py-3 shadow-md',
    'text-13',
    'motion-safe:transition motion-safe:duration-150',
    'data-[state=closed]:motion-safe:opacity-0',
    'data-[swipe=move]:translate-x-[var(--radix-toast-swipe-move-x)]',
    'data-[swipe=cancel]:translate-x-0 data-[swipe=cancel]:motion-safe:transition',
    'data-[swipe=end]:motion-safe:translate-x-[var(--radix-toast-swipe-end-x)]',
  ],
  {
    variants: {
      variant: {
        success:
          'border-success/20 bg-[color-mix(in_srgb,var(--color-success)_8%,transparent)] text-success',
        error:
          'border-danger/20 bg-[color-mix(in_srgb,var(--color-danger)_8%,transparent)] text-danger',
        warning:
          'border-warning/30 bg-[color-mix(in_srgb,var(--color-warning)_15%,transparent)] text-fg',
        info: 'border-border bg-bg-muted text-fg',
        sticky: 'border-border bg-bg-muted text-fg',
      },
    },
    defaultVariants: { variant: 'info' },
  },
);

type ToastItemVariantProps = VariantProps<typeof itemVariants>;

const iconForVariant: Record<ToastVariant, string> = {
  success: 'text-success',
  error: 'text-danger',
  warning: 'text-warning',
  info: 'text-fg-muted',
  sticky: 'text-fg-muted',
};

export const ToastProvider = Toast.Provider;

export type ToastViewportProps = ComponentPropsWithoutRef<typeof Toast.Viewport>;

export const ToastViewport = forwardRef<ElementRef<typeof Toast.Viewport>, ToastViewportProps>(
  function ToastViewport({ className, ...rest }, ref) {
    return (
      <Toast.Viewport
        ref={ref}
        className={cn(
          'fixed bottom-0 right-0 z-toast flex w-full max-w-sm list-none flex-col gap-2 p-4 outline-none',
          'sm:bottom-4 sm:right-4',
          className,
        )}
        {...rest}
      />
    );
  },
);

export interface ToastItemProps
  extends ComponentPropsWithoutRef<typeof Toast.Root>, ToastItemVariantProps {}

export const ToastItem = forwardRef<ElementRef<typeof Toast.Root>, ToastItemProps>(
  function ToastItem({ className, variant, ...rest }, ref) {
    return (
      <Toast.Root
        ref={ref}
        className={cn(itemVariants({ variant }), className)}
        // Error/warning/sticky — role=alert (assertive); остальные — status.
        type={
          variant === 'error' || variant === 'warning' || variant === 'sticky'
            ? 'foreground'
            : 'background'
        }
        {...rest}
      />
    );
  },
);

export const ToastTitle = forwardRef<
  ElementRef<typeof Toast.Title>,
  ComponentPropsWithoutRef<typeof Toast.Title>
>(function ToastTitle({ className, ...rest }, ref) {
  // Цвет наследуется от variant'а через item (text-success/danger/fg). Title
  // оставляем без explicit color — пусть item диктует семантический оттенок.
  return <Toast.Title ref={ref} className={cn('font-medium leading-5', className)} {...rest} />;
});

export const ToastDescription = forwardRef<
  ElementRef<typeof Toast.Description>,
  ComponentPropsWithoutRef<typeof Toast.Description>
>(function ToastDescription({ className, ...rest }, ref) {
  // Description слегка приглушённый (наследует variant-цвет с opacity-80 для
  // дополнительного контраста с title).
  return <Toast.Description ref={ref} className={cn('text-12 opacity-80', className)} {...rest} />;
});

export const ToastAction = forwardRef<
  ElementRef<typeof Toast.Action>,
  ComponentPropsWithoutRef<typeof Toast.Action>
>(function ToastAction({ className, ...rest }, ref) {
  return (
    <Toast.Action
      ref={ref}
      className={cn(
        'ml-auto rounded-md px-2 py-1 text-sm font-medium text-brand-600 hover:bg-brand-50',
        className,
      )}
      {...rest}
    />
  );
});

export const ToastClose = forwardRef<
  ElementRef<typeof Toast.Close>,
  ComponentPropsWithoutRef<typeof Toast.Close>
>(function ToastClose({ className, 'aria-label': ariaLabel = 'Закрыть', ...rest }, ref) {
  return (
    <Toast.Close
      ref={ref}
      aria-label={ariaLabel}
      className={cn(
        'ml-1 rounded-sm p-1 text-fg-muted opacity-70 hover:opacity-100 focus-visible:outline-none focus-visible:ring',
        className,
      )}
      {...rest}
    />
  );
});

interface ToastIconProps extends HTMLAttributes<HTMLSpanElement> {
  variant: ToastVariant;
}

function ToastIcon({ variant, className, ...rest }: ToastIconProps) {
  return (
    <span
      aria-hidden="true"
      className={cn('mt-0.5 shrink-0', iconForVariant[variant], className)}
      {...rest}
    >
      <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
        <circle cx="8" cy="8" r="6" opacity="0.2" />
        <circle cx="8" cy="8" r="3" />
      </svg>
    </span>
  );
}

interface ToasterToastProps {
  record: ToastRecord;
  onDismiss: (id: string) => void;
}

function ToasterToast({ record, onDismiss }: ToasterToastProps) {
  const { id, variant, title, description, duration, action } = record;
  const effectiveDuration = duration ?? 1_000_000_000; // Radix требует число; sticky = практически вечно.
  return (
    <ToastItem
      variant={variant}
      duration={effectiveDuration}
      onOpenChange={(open) => {
        if (!open) onDismiss(id);
      }}
    >
      <ToastIcon variant={variant} />
      <div className="flex flex-1 flex-col gap-0.5">
        <ToastTitle>{title}</ToastTitle>
        {description ? <ToastDescription>{description}</ToastDescription> : null}
      </div>
      {action ? (
        <ToastAction altText={action.label} onClick={() => action.onClick(id)}>
          {action.label}
        </ToastAction>
      ) : null}
      <ToastClose />
    </ToastItem>
  );
}

export interface ToasterProps {
  /** swipe-направление. По умолчанию right (desktop). */
  swipeDirection?: 'right' | 'left' | 'up' | 'down';
  /** Класс для Viewport. */
  viewportClassName?: string;
}

/**
 * Глобальный Toaster. Монтируется один раз в `app/providers` (FE-TASK-030).
 * Читает `useToastStore` и рендерит Radix Toast items.
 */
export function Toaster({ swipeDirection = 'right', viewportClassName }: ToasterProps) {
  const toasts = useToastStore((s) => s.toasts);
  const dismiss = useToastStore((s) => s.dismiss);

  // Если компонент размонтирован — не держим stale toasts.
  useEffect(() => {
    return () => {
      // no-op: clear нежелателен, toasts могут пережить ремоунт (HMR). Оставлено.
    };
  }, []);

  return (
    <ToastProvider swipeDirection={swipeDirection} duration={5000}>
      {toasts.map((record) => (
        <ToasterToast key={record.id} record={record} onDismiss={dismiss} />
      ))}
      <ToastViewport className={viewportClassName} />
    </ToastProvider>
  );
}

export { itemVariants as toastItemVariants };
