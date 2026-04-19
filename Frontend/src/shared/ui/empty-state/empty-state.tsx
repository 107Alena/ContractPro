import { cva, type VariantProps } from 'class-variance-authority';
import { forwardRef, type HTMLAttributes, type ReactNode, useId } from 'react';

import { cn } from '@/shared/lib/cn';

const emptyStateVariants = cva(
  [
    'flex flex-col items-center justify-center gap-3 text-center',
    'rounded-md border border-dashed border-border bg-bg-muted/30 text-fg-muted',
  ],
  {
    variants: {
      size: {
        sm: 'p-6 text-sm',
        md: 'p-10 text-sm',
        lg: 'p-14 text-base',
      },
      tone: {
        neutral: '',
        subtle: 'border-transparent bg-transparent',
      },
    },
    defaultVariants: { size: 'md', tone: 'neutral' },
  },
);

type EmptyStateVariantProps = VariantProps<typeof emptyStateVariants>;

export type EmptyStateHeadingLevel = 'h2' | 'h3' | 'h4' | 'h5' | 'h6';

export interface EmptyStateProps
  extends Omit<HTMLAttributes<HTMLDivElement>, 'title' | 'role'>, EmptyStateVariantProps {
  title: ReactNode;
  description?: ReactNode;
  icon?: ReactNode;
  action?: ReactNode;
  secondaryAction?: ReactNode;
  role?: 'status' | 'alert' | 'region';
  /** Уровень заголовка для корректной иерархии страницы. По умолчанию h2. */
  headingLevel?: EmptyStateHeadingLevel;
}

export const EmptyState = forwardRef<HTMLDivElement, EmptyStateProps>(function EmptyState(
  {
    title,
    description,
    icon,
    action,
    secondaryAction,
    role = 'status',
    headingLevel = 'h2',
    size,
    tone,
    className,
    ...rest
  },
  ref,
) {
  const reactId = useId();
  const titleId = `${reactId}-title`;
  const descriptionId = description ? `${reactId}-description` : undefined;
  const Heading = headingLevel;

  return (
    <div
      ref={ref}
      role={role}
      aria-live={role === 'status' ? 'polite' : undefined}
      aria-labelledby={titleId}
      aria-describedby={descriptionId}
      className={cn(emptyStateVariants({ size, tone }), className)}
      {...rest}
    >
      {icon ? (
        <div aria-hidden="true" className="text-fg-muted">
          {icon}
        </div>
      ) : null}
      <Heading id={titleId} className="font-medium text-fg">
        {title}
      </Heading>
      {description ? (
        <p id={descriptionId} className="max-w-md text-fg-muted">
          {description}
        </p>
      ) : null}
      {action || secondaryAction ? (
        <div role="group" className="mt-2 flex flex-wrap items-center justify-center gap-2">
          {action}
          {secondaryAction}
        </div>
      ) : null}
    </div>
  );
});

export { emptyStateVariants };
