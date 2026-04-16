import { cva, type VariantProps } from 'class-variance-authority';
import { forwardRef, type InputHTMLAttributes } from 'react';

import { cn } from '@/shared/lib/cn';

const inputVariants = cva(
  [
    'block w-full rounded-md border bg-bg text-fg',
    'px-3 h-10 text-sm leading-5',
    'placeholder:text-fg-muted',
    'transition-colors duration-150',
    'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-0',
    'disabled:cursor-not-allowed disabled:bg-bg-muted disabled:text-fg-muted',
  ],
  {
    variants: {
      state: {
        default: 'border-border focus-visible:border-brand-500',
        error: 'border-danger focus-visible:border-danger focus-visible:ring-danger/60',
      },
      size: {
        sm: 'h-8 text-sm',
        md: 'h-10 text-sm',
        lg: 'h-12 text-base',
      },
    },
    defaultVariants: { state: 'default', size: 'md' },
  },
);

type InputVariantProps = VariantProps<typeof inputVariants>;

// aria-describedby для hint/error id передаётся извне (каллер управляет текстом ошибки).
// FormField-обёртка с авто-биндингом Label+Error появится в FE-TASK-025 (RHF+Zod).
export interface InputProps
  extends Omit<InputHTMLAttributes<HTMLInputElement>, 'size'>, Omit<InputVariantProps, 'state'> {
  error?: boolean;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { className, error = false, size, type = 'text', ...rest },
  ref,
) {
  const state = error ? 'error' : 'default';
  return (
    <input
      ref={ref}
      type={type}
      aria-invalid={error || undefined}
      className={cn(inputVariants({ state, size }), className)}
      {...rest}
    />
  );
});

export { inputVariants };
