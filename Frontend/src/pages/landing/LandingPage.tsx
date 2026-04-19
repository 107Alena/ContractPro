// LandingPage (FE-TASK-041) — публичная стартовая страница `/`. Статичная,
// без запросов к API и auth-guard'ов. Композирует 4 секции (§17.4):
//   HeroSection → FeaturesSection → PricingSection → FAQAccordion.
//
// CTA ведут на /login (FE-TASK-029). Контент (копия, тарифы, FAQ) вынесен в
// content.ts — секции остаются чистыми presentational-компонентами и могут
// получать свои данные через props (see stories).
//
// Доступность: каждая секция — <section aria-labelledby=...> с собственным <h2>,
// единственный <h1> на странице — в HeroSection. Якоря #features / #pricing /
// #faq стабильны и позволяют deep-link'ам.
import { FAQAccordion } from './sections/FAQAccordion';
import { FeaturesSection } from './sections/FeaturesSection';
import { HeroSection } from './sections/HeroSection';
import { PricingSection } from './sections/PricingSection';

export function LandingPage(): JSX.Element {
  return (
    <main data-testid="page-landing" className="flex flex-col">
      <HeroSection />
      <FeaturesSection />
      <PricingSection />
      <FAQAccordion />
    </main>
  );
}
