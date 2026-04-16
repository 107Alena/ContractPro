import { Slot } from '@radix-ui/react-slot';
import { cva, type VariantProps } from 'class-variance-authority';
import { type ButtonHTMLAttributes, forwardRef, type MouseEvent, type ReactNode } from 'react';

import { cn } from '@/shared/lib/cn';
import { Spinner } from '@/shared/ui/spinner';

const buttonVariants = cva(
  [
    'inline-flex items-center justify-center gap-2 whitespace-nowrap select-none',
    'font-sans font-medium rounded-md',
    'transition-colors duration-150',
    'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-2',
    'disabled:pointer-events-none disabled:opacity-50',
    // aria-busy не входит в дефолтный набор aria-variants Tailwind 3.4 — используем
    // data-[loading] (проставляется React-ом на корень), плюс aria-disabled для asChild-ветки.
    'data-[loading]:pointer-events-none data-[loading]:cursor-progress',
    'aria-disabled:pointer-events-none aria-disabled:opacity-50',
  ],
  {
    variants: {
      variant: {
        primary: 'bg-brand-500 text-white hover:bg-brand-600 active:bg-brand-600',
        secondary:
          'bg-bg-muted text-fg border border-border hover:bg-brand-50 active:bg-brand-50 active:border-brand-500',
        ghost: 'bg-transparent text-fg hover:bg-bg-muted active:bg-brand-50',
        danger: 'bg-danger text-white hover:brightness-110 active:brightness-95',
      },
      size: {
        sm: 'h-8 px-3 text-sm',
        md: 'h-10 px-4 text-sm',
        lg: 'h-12 px-5 text-base',
      },
      fullWidth: { true: 'w-full', false: '' },
    },
    defaultVariants: { variant: 'primary', size: 'md', fullWidth: false },
  },
);

type ButtonVariantProps = VariantProps<typeof buttonVariants>;

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement>, ButtonVariantProps {
  asChild?: boolean;
  loading?: boolean;
  iconLeft?: ReactNode;
  iconRight?: ReactNode;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  {
    className,
    variant,
    size,
    fullWidth,
    asChild = false,
    loading = false,
    disabled = false,
    iconLeft,
    iconRight,
    children,
    type,
    onClick,
    ...rest
  },
  ref,
) {
  const Comp = asChild ? Slot : 'button';
  const isDisabled = disabled || loading;

  // Slot пробрасывает disabled в ребёнка; для <a>/<div> это невалидный HTML и не блокирует
  // активацию. При asChild используем aria-disabled + tabIndex=-1 и глушим onClick.
  const a11yDisabledProps = isDisabled
    ? asChild
      ? { 'aria-disabled': true, tabIndex: -1 }
      : { disabled: true }
    : {};

  function handleClick(e: MouseEvent<HTMLButtonElement>) {
    if (isDisabled) {
      e.preventDefault();
      e.stopPropagation();
      return;
    }
    onClick?.(e);
  }

  return (
    <Comp
      ref={ref}
      className={cn(buttonVariants({ variant, size, fullWidth }), className)}
      aria-busy={loading || undefined}
      data-loading={loading || undefined}
      type={asChild ? undefined : (type ?? 'button')}
      onClick={handleClick}
      {...a11yDisabledProps}
      {...rest}
    >
      {loading ? <Spinner size={size === 'lg' ? 'md' : 'sm'} /> : iconLeft}
      {children}
      {!loading && iconRight}
    </Comp>
  );
});

export { buttonVariants };
