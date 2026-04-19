// PricingSection — карточки тарифов. featured-карточка визуально выделена
// (border-brand, Badge «Популярный», primary-CTA). Высоты карточек выравниваются
// через items-stretch + flex-col.
import { Link } from 'react-router-dom';

import { Badge, buttonVariants } from '@/shared/ui';

import { PRICING_PLANS, type PricingPlan } from '../content';

export interface PricingSectionProps {
  plans?: PricingPlan[];
}

export function PricingSection({ plans = PRICING_PLANS }: PricingSectionProps): JSX.Element {
  return (
    <section
      id="pricing"
      aria-labelledby="pricing-title"
      className="bg-bg-muted py-16 md:py-20 lg:py-24"
    >
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-10 px-4">
        <header className="flex flex-col gap-3 text-center">
          <p className="text-sm font-semibold uppercase tracking-wider text-brand-600">Тарифы</p>
          <h2 id="pricing-title" className="text-2xl font-semibold text-fg md:text-3xl">
            Начните бесплатно, растите с командой
          </h2>
          <p className="mx-auto max-w-2xl text-base text-fg-muted">
            Без скрытых условий. Платите только за объём проверок, который вам нужен.
          </p>
        </header>

        <ul className="grid grid-cols-1 items-stretch gap-4 md:grid-cols-3">
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
  const cardClassName = [
    'flex h-full flex-col gap-5 rounded-lg border bg-bg p-6 shadow-sm',
    isFeatured ? 'border-brand-500 ring-1 ring-brand-500' : 'border-border',
  ].join(' ');

  return (
    <li className={cardClassName}>
      <header className="flex flex-col gap-2">
        <div className="flex items-center justify-between gap-2">
          <h3 className="text-lg font-semibold text-fg">{plan.name}</h3>
          {plan.badge ? <Badge variant="brand">{plan.badge}</Badge> : null}
        </div>
        <p className="text-sm text-fg-muted">{plan.description}</p>
      </header>

      <div className="flex items-baseline gap-2">
        <span className="text-3xl font-semibold text-fg">{plan.price}</span>
        <span className="text-sm text-fg-muted">{plan.priceHint}</span>
      </div>

      <ul className="flex flex-1 flex-col gap-2 text-sm text-fg">
        {plan.bullets.map((bullet, index) => (
          // key по (plan.id, index) — bullets внутри плана уникальны, но композитный
          // key устойчив к дублирующимся строкам между планами.
          <li key={`${plan.id}-${index}`} className="flex items-start gap-2">
            <CheckIcon />
            <span>{bullet}</span>
          </li>
        ))}
      </ul>

      <Link
        to={plan.cta.to}
        className={buttonVariants({
          variant: isFeatured ? 'primary' : 'secondary',
          size: 'md',
          fullWidth: true,
        })}
      >
        {plan.cta.label}
      </Link>
    </li>
  );
}

function CheckIcon(): JSX.Element {
  return (
    <svg
      viewBox="0 0 20 20"
      width="16"
      height="16"
      fill="none"
      aria-hidden="true"
      focusable="false"
      className="mt-0.5 shrink-0 text-brand-600"
    >
      <path
        d="m5 10 3.5 3.5L15 6.5"
        stroke="currentColor"
        strokeWidth="1.75"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
