// HeroSection — верхний экран LandingPage: заголовок, подзаголовок, пара CTA,
// trust-бейджи. CTA — <Link className={buttonVariants(...)}> (§FE-TASK-042
// deviation: asChild+Link в jsdom падает на React.Children.only).
import { Link } from 'react-router-dom';

import { buttonVariants } from '@/shared/ui';

import { HERO_CONTENT, type HeroContent } from '../content';

export interface HeroSectionProps {
  content?: HeroContent;
}

export function HeroSection({ content = HERO_CONTENT }: HeroSectionProps): JSX.Element {
  return (
    <section
      id="hero"
      aria-labelledby="hero-title"
      className="relative overflow-hidden bg-gradient-to-b from-brand-50 to-bg"
    >
      <div className="mx-auto flex w-full max-w-5xl flex-col items-center gap-6 px-4 py-16 text-center sm:py-20 md:py-24 lg:py-28">
        <p className="text-sm font-semibold uppercase tracking-wider text-brand-600">
          {content.eyebrow}
        </p>
        <h1
          id="hero-title"
          className="text-3xl font-semibold text-fg sm:text-4xl md:text-5xl md:leading-tight"
        >
          {content.title}
        </h1>
        <p className="max-w-2xl text-base text-fg-muted md:text-lg">{content.subtitle}</p>

        <div className="mt-2 flex flex-col items-stretch gap-3 sm:flex-row sm:items-center sm:justify-center">
          <Link
            to={content.primaryCta.to}
            className={buttonVariants({ variant: 'primary', size: 'lg' })}
          >
            {content.primaryCta.label}
          </Link>
          <Link
            to={content.secondaryCta.to}
            className={buttonVariants({ variant: 'secondary', size: 'lg' })}
          >
            {content.secondaryCta.label}
          </Link>
        </div>

        {content.trustBadges.length > 0 ? (
          <ul
            aria-label="Ключевые характеристики"
            className="mt-6 flex flex-wrap items-center justify-center gap-x-6 gap-y-2 text-xs text-fg-muted"
          >
            {content.trustBadges.map((badge) => (
              <li key={badge} className="flex items-center gap-2">
                <span aria-hidden="true" className="h-1.5 w-1.5 rounded-full bg-brand-500" />
                {badge}
              </li>
            ))}
          </ul>
        ) : null}
      </div>
    </section>
  );
}
