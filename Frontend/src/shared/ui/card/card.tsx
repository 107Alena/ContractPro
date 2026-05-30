import { cva, type VariantProps } from 'class-variance-authority';
import { type ElementType, type HTMLAttributes } from 'react';

import { cn } from '@/shared/lib/cn';

// Card — карточная поверхность дашборда и списков (Figma 84:2).
// Белый фон + мягкая тень `shadow-sm` (≈ 0 1px 3px rgba(0,0,0,.04)) + без бордера.
// Источник правды для card-рецепта — переиспользуется на всех экранах (4.3–4.9).
//
// radius:
//   card (12px) — контентные карточки (LastCheck, Сводка, Организация, ActionCard);
//   md   (10px) — мелкие risk-карточки (KeyRisks);
//   xl   (16px) — hero/welcome поверхность.
// 12px остаётся arbitrary (`rounded-[12px]`) по прецеденту modal (ADR-FE-09): нет
// семантического слота между md(10) и lg(14). Padding/gap задаёт потребитель через className.
const cardVariants = cva('bg-bg shadow-sm', {
  variants: {
    radius: {
      card: 'rounded-[12px]',
      md: 'rounded-md',
      xl: 'rounded-xl',
    },
  },
  defaultVariants: { radius: 'card' },
});

export interface CardProps extends HTMLAttributes<HTMLElement>, VariantProps<typeof cardVariants> {
  /** Тег-обёртка. По умолчанию `section` (landmark с aria-label). Для вложенных карточек — `article`. */
  as?: ElementType;
}

export function Card({ as: Tag = 'section', radius, className, ...rest }: CardProps): JSX.Element {
  return <Tag className={cn(cardVariants({ radius }), className)} {...rest} />;
}

export { cardVariants };
