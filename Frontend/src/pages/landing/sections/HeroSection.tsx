// HeroSection — верхний экран LandingPage (Figma node 12:2). Badge-eyebrow
// (Brand-pill) + 60px заголовок + 20px подзаголовок + два больших CTA +
// ProductMockup. Figma выровнен: bg #fafafc (≈ bg-bg-muted), gap-14 (56px)
// между HeroContent и mockup'ом, p-20 (80px) — на md+ breakpoint'ах.
//
// CTA — <Link className={buttonVariants(...)}> (FE-TASK-042 deviation:
// asChild+Link в jsdom падает на React.Children.only). Hero-CTA крупнее
// дефолтного `lg` Button'а (px-8 py-4 text-17 rounded-xl + drop shadow),
// поэтому используем inline-классы вместо нового size'а — это
// специфический визуал именно для Hero.
import { Link } from 'react-router-dom';

import { cn } from '@/shared/lib/cn';
import { Badge, buttonVariants } from '@/shared/ui';

import { HERO_CONTENT, type HeroContent } from '../content';
import { HeroProductMockup } from './HeroProductMockup';

export interface HeroSectionProps {
  content?: HeroContent;
}

// Hero-CTA — переопределение размеров Button'а под Figma node 12:9 / 12:11.
const HERO_CTA_OVERRIDE = 'h-auto px-8 py-4 text-17 rounded-xl';
const HERO_CTA_PRIMARY_SHADOW = 'shadow-[0_4px_16px_0_rgba(245,94,18,0.25)]';

export function HeroSection({ content = HERO_CONTENT }: HeroSectionProps): JSX.Element {
  return (
    <section
      id="hero"
      aria-labelledby="hero-title"
      className="bg-bg-muted px-4 py-16 sm:py-20 md:py-24 lg:px-20 lg:py-20"
    >
      <div className="mx-auto flex w-full max-w-[1280px] flex-col items-center gap-14">
        <div className="flex flex-col items-center gap-6 text-center">
          <Badge variant="brand" size="md">
            {content.eyebrow}
          </Badge>
          <h1
            id="hero-title"
            className="text-4xl font-bold leading-[1.13] tracking-[-0.5px] text-fg sm:text-5xl md:text-60 md:leading-[68px] md:tracking-[-1.5px]"
          >
            {content.title}
          </h1>
          <p className="max-w-3xl text-base leading-relaxed text-fg-muted md:text-20 md:leading-[30px]">
            {content.subtitle}
          </p>

          <div className="mt-2 flex flex-col items-stretch gap-3 sm:flex-row sm:items-center sm:justify-center sm:gap-4">
            <Link
              to={content.primaryCta.to}
              className={cn(
                buttonVariants({ variant: 'primary', size: 'lg' }),
                HERO_CTA_OVERRIDE,
                HERO_CTA_PRIMARY_SHADOW,
              )}
            >
              {content.primaryCta.label}
            </Link>
            <Link
              to={content.secondaryCta.to}
              className={cn(
                buttonVariants({ variant: 'secondary', size: 'lg' }),
                HERO_CTA_OVERRIDE,
              )}
            >
              {content.secondaryCta.label}
            </Link>
          </div>
        </div>

        <HeroProductMockup />
      </div>
    </section>
  );
}
