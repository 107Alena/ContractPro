import * as Dialog from '@radix-ui/react-dialog';
import { cva, type VariantProps } from 'class-variance-authority';
import {
  type ComponentPropsWithoutRef,
  type ElementRef,
  forwardRef,
  type HTMLAttributes,
} from 'react';

import { cn } from '@/shared/lib/cn';

const contentVariants = cva(
  [
    'fixed left-1/2 top-1/2 z-modal -translate-x-1/2 -translate-y-1/2',
    'flex max-h-[85vh] w-[calc(100vw-2rem)] flex-col',
    'rounded-lg bg-bg text-fg shadow-lg outline-none',
    'focus-visible:ring focus-visible:ring-offset-0',
    'motion-safe:transition motion-safe:duration-150 motion-safe:ease-out',
    'data-[state=closed]:motion-safe:opacity-0 data-[state=closed]:motion-safe:scale-95',
  ],
  {
    variants: {
      size: {
        sm: 'sm:max-w-sm',
        md: 'sm:max-w-md',
        lg: 'sm:max-w-2xl',
      },
    },
    defaultVariants: { size: 'md' },
  },
);

const overlayClasses = cn(
  'fixed inset-0 z-modal bg-fg/40',
  'motion-safe:transition-opacity motion-safe:duration-150',
  'data-[state=closed]:motion-safe:opacity-0',
);

export type ModalRootProps = Dialog.DialogProps;

export const Modal = Dialog.Root;
export const ModalTrigger = Dialog.Trigger;
export const ModalClose = Dialog.Close;
export const ModalPortal = Dialog.Portal;

export const ModalOverlay = forwardRef<
  ElementRef<typeof Dialog.Overlay>,
  ComponentPropsWithoutRef<typeof Dialog.Overlay>
>(function ModalOverlay({ className, ...rest }, ref) {
  return <Dialog.Overlay ref={ref} className={cn(overlayClasses, className)} {...rest} />;
});

export interface ModalContentProps
  extends ComponentPropsWithoutRef<typeof Dialog.Content>, VariantProps<typeof contentVariants> {
  /** При `false` клик по overlay не закрывает модалку (отключение onPointerDownOutside). */
  dismissOnOverlay?: boolean;
  /** При `true` убирает закрытие по ESC. Используется для критичных confirm-modal. */
  disableEscape?: boolean;
  overlayClassName?: string;
}

export const ModalContent = forwardRef<ElementRef<typeof Dialog.Content>, ModalContentProps>(
  function ModalContent(
    {
      className,
      overlayClassName,
      size,
      dismissOnOverlay = true,
      disableEscape = false,
      children,
      onPointerDownOutside,
      onEscapeKeyDown,
      ...rest
    },
    ref,
  ) {
    return (
      <ModalPortal>
        <ModalOverlay className={overlayClassName} />
        <Dialog.Content
          ref={ref}
          className={cn(contentVariants({ size }), className)}
          onPointerDownOutside={(event) => {
            if (!dismissOnOverlay) event.preventDefault();
            onPointerDownOutside?.(event);
          }}
          onEscapeKeyDown={(event) => {
            if (disableEscape) event.preventDefault();
            onEscapeKeyDown?.(event);
          }}
          {...rest}
        >
          {children}
        </Dialog.Content>
      </ModalPortal>
    );
  },
);

export const ModalHeader = forwardRef<HTMLDivElement, HTMLAttributes<HTMLDivElement>>(
  function ModalHeader({ className, ...rest }, ref) {
    return (
      <div
        ref={ref}
        className={cn('flex flex-col gap-1 border-b border-border px-6 py-4', className)}
        {...rest}
      />
    );
  },
);

export const ModalTitle = forwardRef<
  ElementRef<typeof Dialog.Title>,
  ComponentPropsWithoutRef<typeof Dialog.Title>
>(function ModalTitle({ className, ...rest }, ref) {
  return (
    <Dialog.Title
      ref={ref}
      className={cn('text-lg font-semibold leading-6 text-fg', className)}
      {...rest}
    />
  );
});

export const ModalDescription = forwardRef<
  ElementRef<typeof Dialog.Description>,
  ComponentPropsWithoutRef<typeof Dialog.Description>
>(function ModalDescription({ className, ...rest }, ref) {
  return (
    <Dialog.Description ref={ref} className={cn('text-sm text-fg-muted', className)} {...rest} />
  );
});

export const ModalBody = forwardRef<HTMLDivElement, HTMLAttributes<HTMLDivElement>>(
  function ModalBody({ className, ...rest }, ref) {
    return (
      <div
        ref={ref}
        className={cn('flex-1 overflow-y-auto px-6 py-4 text-sm text-fg', className)}
        {...rest}
      />
    );
  },
);

export const ModalFooter = forwardRef<HTMLDivElement, HTMLAttributes<HTMLDivElement>>(
  function ModalFooter({ className, ...rest }, ref) {
    return (
      <div
        ref={ref}
        className={cn(
          'flex flex-col-reverse gap-2 border-t border-border px-6 py-4 sm:flex-row sm:justify-end',
          className,
        )}
        {...rest}
      />
    );
  },
);

export { contentVariants as modalContentVariants };
