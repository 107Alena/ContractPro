import { Slot } from '@radix-ui/react-slot';
import { cva, type VariantProps } from 'class-variance-authority';
import { type ButtonHTMLAttributes, forwardRef, type MouseEvent, type ReactNode } from 'react';

import { cn } from '@/shared/lib/cn';
import { Spinner } from '@/shared/ui/spinner';

const buttonVariants = cva(
  [
    'inline-flex items-center justify-center gap-2 whitespace-nowrap select-none',
    'font-sans rounded-md',
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
      // Веса распределены по figma-выборке: Primary CTA / Danger — semibold (font-600);
      // Secondary / Ghost — medium (font-500). См. Figma node-ids 6:16 (Primary CTA),
      // 6:14 (Secondary Login) — §8.2 high-architecture.md.
      variant: {
        primary: 'bg-brand-500 text-white font-semibold hover:bg-brand-600 active:bg-brand-600',
        secondary:
          'bg-bg text-fg font-medium border-[1.5px] border-border hover:bg-brand-50 active:bg-brand-50 active:border-brand-500',
        ghost: 'bg-transparent text-fg font-medium hover:bg-bg-muted active:bg-brand-50',
        danger: 'bg-danger text-white font-semibold hover:brightness-110 active:brightness-95',
      },
      // md выровнен с Figma (h:38px, py:10px, text:15px). sm/lg — внутренние варианты
      // без figma-референса; trafic light: sm — компактные действия в таблицах,
      // lg — full-width формы.
      size: {
        sm: 'h-8 px-3 text-13',
        md: 'h-[38px] px-5 py-2.5 text-15',
        lg: 'h-12 px-6 text-16',
      },
      fullWidth: { true: 'w-full', false: '' },
    },
    // Primary/Danger CTA в md по figma имеют +4px горизонтального padding для
    // визуального приоритета над secondary/ghost. См. Figma node 6:16 — Primary CTA
    // (px:24) vs 6:14 — Secondary Login (px:20).
    compoundVariants: [
      { variant: 'primary', size: 'md', class: 'px-6' },
      { variant: 'danger', size: 'md', class: 'px-6' },
    ],
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

  const sharedProps = {
    className: cn(buttonVariants({ variant, size, fullWidth }), className),
    'aria-busy': loading || undefined,
    'data-loading': loading || undefined,
    onClick: handleClick,
    ...a11yDisabledProps,
    ...rest,
  };

  // При asChild оборачиваем единственный child от потребителя через Radix Slot —
  // injection icon/spinner недоступен (Slot.Children.only требует ровно 1 элемент);
  // потребитель размещает иконки внутри своего <a>/<Link> сам.
  if (asChild) {
    return (
      <Slot ref={ref} {...sharedProps}>
        {children}
      </Slot>
    );
  }

  return (
    <button ref={ref} type={type ?? 'button'} {...sharedProps}>
      {loading ? <Spinner size={size === 'lg' ? 'md' : 'sm'} /> : iconLeft}
      {children}
      {!loading && iconRight}
    </button>
  );
});

export { buttonVariants };
