import { Slot } from '@radix-ui/react-slot';
import { cva, type VariantProps } from 'class-variance-authority';
import {
  type AnchorHTMLAttributes,
  type ComponentPropsWithoutRef,
  forwardRef,
  Fragment,
  type HTMLAttributes,
  type ReactNode,
} from 'react';

import { cn } from '@/shared/lib/cn';

const breadcrumbsVariants = cva('text-sm text-fg-muted', {
  variants: {
    size: {
      sm: 'text-xs',
      md: 'text-sm',
    },
  },
  defaultVariants: { size: 'md' },
});

const breadcrumbsLinkVariants = cva(
  [
    'inline-flex items-center gap-1 rounded-sm',
    'text-fg-muted transition-colors',
    'hover:text-fg hover:underline',
    'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-1',
  ],
  {
    variants: {},
    defaultVariants: {},
  },
);

const breadcrumbsPageVariants = cva(
  ['inline-flex items-center gap-1 font-medium text-fg', 'aria-[current=page]:text-fg'],
  {
    variants: {},
    defaultVariants: {},
  },
);

export interface BreadcrumbItem {
  id?: string;
  label: ReactNode;
  href?: string;
  icon?: ReactNode;
  current?: boolean;
}

export interface BreadcrumbsProps
  extends Omit<HTMLAttributes<HTMLElement>, 'children'>, VariantProps<typeof breadcrumbsVariants> {
  items: ReadonlyArray<BreadcrumbItem>;
  separator?: ReactNode;
  /** aria-label корневого <nav>, default «Хлебные крошки». */
  label?: string;
  /** Если items.length превышает это число — середина сворачивается в «…». */
  maxItems?: number;
  /** Сколько элементов показать слева при сворачивании. По умолчанию 1. */
  itemsBeforeCollapse?: number;
  /** Сколько элементов показать справа при сворачивании. По умолчанию 1. */
  itemsAfterCollapse?: number;
}

function DefaultSeparator() {
  return <span>/</span>;
}

export const Breadcrumbs = forwardRef<HTMLElement, BreadcrumbsProps>(function Breadcrumbs(
  {
    items,
    separator,
    label = 'Хлебные крошки',
    maxItems,
    itemsBeforeCollapse = 1,
    itemsAfterCollapse = 1,
    className,
    size,
    ...rest
  },
  ref,
) {
  const shouldCollapse =
    typeof maxItems === 'number' &&
    items.length > maxItems &&
    items.length > itemsBeforeCollapse + itemsAfterCollapse;

  const visibleItems = shouldCollapse
    ? [
        ...items.slice(0, itemsBeforeCollapse),
        { id: '__ellipsis__', label: '…', current: false } as BreadcrumbItem,
        ...items.slice(items.length - itemsAfterCollapse),
      ]
    : [...items];

  const lastIdx = visibleItems.length - 1;
  const sep = separator ?? <DefaultSeparator />;

  return (
    <BreadcrumbsRoot
      ref={ref}
      label={label}
      className={cn(breadcrumbsVariants({ size }), className)}
      {...rest}
    >
      <BreadcrumbsList>
        {visibleItems.map((item, idx) => {
          const isLast = idx === lastIdx;
          const isEllipsis = item.id === '__ellipsis__';
          const isCurrent = item.current === true || (isLast && !isEllipsis);

          return (
            <Fragment key={item.id ?? `bc-${idx}`}>
              <BreadcrumbsItem>
                {isEllipsis ? (
                  <BreadcrumbsEllipsis />
                ) : isCurrent || !item.href ? (
                  <BreadcrumbsPage aria-current={isCurrent ? 'page' : undefined}>
                    {item.icon ? (
                      <span aria-hidden="true" className="inline-flex">
                        {item.icon}
                      </span>
                    ) : null}
                    {item.label}
                  </BreadcrumbsPage>
                ) : (
                  <BreadcrumbsLink href={item.href}>
                    {item.icon ? (
                      <span aria-hidden="true" className="inline-flex">
                        {item.icon}
                      </span>
                    ) : null}
                    {item.label}
                  </BreadcrumbsLink>
                )}
              </BreadcrumbsItem>
              {isLast ? null : <BreadcrumbsSeparator>{sep}</BreadcrumbsSeparator>}
            </Fragment>
          );
        })}
      </BreadcrumbsList>
    </BreadcrumbsRoot>
  );
});

export interface BreadcrumbsRootProps extends HTMLAttributes<HTMLElement> {
  label?: string;
}

export const BreadcrumbsRoot = forwardRef<HTMLElement, BreadcrumbsRootProps>(
  function BreadcrumbsRoot({ label = 'Хлебные крошки', className, children, ...rest }, ref) {
    return (
      <nav
        ref={ref}
        aria-label={label}
        className={cn(breadcrumbsVariants({}), className)}
        {...rest}
      >
        {children}
      </nav>
    );
  },
);

export const BreadcrumbsList = forwardRef<HTMLOListElement, HTMLAttributes<HTMLOListElement>>(
  function BreadcrumbsList({ className, ...rest }, ref) {
    return (
      <ol ref={ref} className={cn('flex flex-wrap items-center gap-1.5', className)} {...rest} />
    );
  },
);

export const BreadcrumbsItem = forwardRef<HTMLLIElement, HTMLAttributes<HTMLLIElement>>(
  function BreadcrumbsItem({ className, ...rest }, ref) {
    return <li ref={ref} className={cn('inline-flex items-center gap-1.5', className)} {...rest} />;
  },
);

export interface BreadcrumbsLinkProps extends AnchorHTMLAttributes<HTMLAnchorElement> {
  asChild?: boolean;
}

export const BreadcrumbsLink = forwardRef<HTMLAnchorElement, BreadcrumbsLinkProps>(
  function BreadcrumbsLink({ asChild = false, className, children, ...rest }, ref) {
    if (asChild) {
      return (
        <Slot ref={ref} className={cn(breadcrumbsLinkVariants(), className)} {...rest}>
          {children}
        </Slot>
      );
    }
    return (
      <a ref={ref} className={cn(breadcrumbsLinkVariants(), className)} {...rest}>
        {children}
      </a>
    );
  },
);

export const BreadcrumbsPage = forwardRef<HTMLSpanElement, ComponentPropsWithoutRef<'span'>>(
  function BreadcrumbsPage({ className, ...rest }, ref) {
    const ariaCurrent = 'aria-current' in rest ? rest['aria-current'] : 'page';
    return (
      <span
        ref={ref}
        className={cn(breadcrumbsPageVariants(), className)}
        {...rest}
        aria-current={ariaCurrent}
      />
    );
  },
);

export const BreadcrumbsSeparator = forwardRef<HTMLSpanElement, ComponentPropsWithoutRef<'span'>>(
  function BreadcrumbsSeparator({ className, children, ...rest }, ref) {
    return (
      <span
        ref={ref}
        role="presentation"
        aria-hidden="true"
        className={cn('text-fg-muted', className)}
        {...rest}
      >
        {children ?? <DefaultSeparator />}
      </span>
    );
  },
);

export const BreadcrumbsEllipsis = forwardRef<HTMLSpanElement, ComponentPropsWithoutRef<'span'>>(
  function BreadcrumbsEllipsis({ className, ...rest }, ref) {
    return (
      <span ref={ref} aria-hidden="true" className={cn('px-1 text-fg-muted', className)} {...rest}>
        …
      </span>
    );
  },
);

export { breadcrumbsLinkVariants, breadcrumbsPageVariants, breadcrumbsVariants };
