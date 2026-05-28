import { cva, type VariantProps } from 'class-variance-authority';
import { forwardRef, type InputHTMLAttributes } from 'react';

import { cn } from '@/shared/lib/cn';

// Figma-aligned: bg-white / border-border (1px) / rounded-md (10px) / px-4 (16px)
// / text-15 (Inter Regular). Placeholder темнее fg-muted, ближе к figma #a6abb2 →
// text-fg-disabled (#999ea6). Source: nodes 56:10, 58:15, 59:6, 59:28 (Auth Desktop).
const inputVariants = cva(
  [
    'block w-full rounded-md border bg-bg text-fg',
    'px-4 h-10 text-15 leading-5',
    'placeholder:text-fg-disabled',
    'transition-colors duration-150',
    'focus-visible:outline-none focus-visible:ring focus-visible:ring-offset-0',
    // disabled: opacity-70 на весь input (figma) + smoother text muting.
    'disabled:cursor-not-allowed disabled:bg-bg-muted disabled:text-fg-subtle disabled:opacity-70',
  ],
  {
    variants: {
      state: {
        default: 'border-border focus-visible:border-brand-500',
        // Error: 1.5px borderwidth + light danger tint bg (figma node 58:15).
        error:
          'border-[1.5px] border-danger bg-danger-bg focus-visible:border-danger focus-visible:ring-danger/60',
      },
      size: {
        sm: 'h-8 text-13',
        md: 'h-10 text-15',
        lg: 'h-12 text-16',
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
