// PricingSection — Figma node 21:2. 3 тарифные карточки: Free / Pro (featured,
// DARK card) / Plus. Featured card имеет ТЁМНЫЙ фон bg-fg + drop-shadow brand,
// остальные — белые с border-subtle.
import { Link } from 'react-router-dom';

import { cn } from '@/shared/lib/cn';
import { buttonVariants } from '@/shared/ui';

import { PRICING_PLANS, type PricingPlan } from '../content';

export interface PricingSectionProps {
  plans?: PricingPlan[];
}

export function PricingSection({ plans = PRICING_PLANS }: PricingSectionProps): JSX.Element {
  return (
    <section
      id="pricing"
      aria-labelledby="pricing-title"
      className="bg-bg-muted px-4 py-16 sm:py-20 lg:px-20"
    >
      <div className="mx-auto flex w-full max-w-[1280px] flex-col items-center gap-12">
        <header className="flex flex-col items-center gap-4 text-center">
          <p className="text-14 font-semibold tracking-[2px] text-brand-500">ТАРИФЫ</p>
          <h2
            id="pricing-title"
            className="text-3xl font-bold leading-[1.1] tracking-[-0.5px] text-fg sm:text-4xl md:text-[44px] md:tracking-[-1px]"
          >
            Выберите подходящий план
          </h2>
          <p className="text-18 text-fg-muted">Начните бесплатно — масштабируйте по мере роста</p>
        </header>

        <ul className="grid w-full grid-cols-1 items-stretch gap-6 lg:grid-cols-3">
          {plans.map((plan) => (
            <PlanCard key={plan.id} plan={plan} />
          ))}
        </ul>
      </div>
    </section>
  );
}

function PlanCard({ plan }: { plan: PricingPlan }): JSX.Element {
  const isFeatured = plan.featured === true;
  return (
    <li
      className={cn(
        'flex h-full flex-col gap-6 rounded-[20px] px-8 py-9',
        isFeatured
          ? 'bg-fg shadow-[0_8px_32px_0_rgba(245,94,18,0.15)]'
          : 'border border-border-subtle bg-bg',
      )}
    >
      <h3
        className={cn(
          'font-semibold leading-none text-brand-500',
          isFeatured ? 'text-[40px]' : 'text-[32px]',
        )}
      >
        {plan.name}
      </h3>

      <div className="flex items-baseline gap-1">
        <span
          className={cn(
            'text-[36px] font-bold leading-none',
            isFeatured ? 'text-white' : 'text-fg',
          )}
        >
          {plan.price}
        </span>
        {plan.priceHint ? (
          <span className={cn('text-16', isFeatured ? 'text-fg-disabled' : 'text-fg-muted')}>
            {plan.priceHint}
          </span>
        ) : null}
      </div>

      <p className={cn('text-15', isFeatured ? 'text-fg-disabled' : 'text-fg-muted')}>
        {plan.description}
      </p>

      <div className={cn('h-px w-full', isFeatured ? 'bg-white/10' : 'bg-border-subtle')} />

      <ul className="flex flex-1 flex-col gap-3">
        {plan.bullets.map((bullet, index) => (
          <li key={`${plan.id}-${index}`} className="flex items-center gap-2.5">
            <span
              aria-hidden="true"
              className={cn('font-medium text-14', isFeatured ? 'text-brand-500' : 'text-success')}
            >
              ✓
            </span>
            <span className={cn('text-15', isFeatured ? 'text-border' : 'text-fg-muted')}>
              {bullet}
            </span>
          </li>
        ))}
      </ul>

      <Link
        to={plan.cta.to}
        className={cn(
          buttonVariants({
            variant: isFeatured ? 'primary' : 'secondary',
            size: 'md',
            fullWidth: true,
          }),
          'h-auto rounded-xl py-3.5 text-16',
        )}
      >
        {plan.cta.label}
      </Link>
    </li>
  );
}
