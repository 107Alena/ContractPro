// FAQAccordion — Figma node 23:2 (FAQ Section) + 23:6 (FAQList).
// Использует shared/ui Accordion (Radix Accordion) с figma-aligned overrides:
// больший padding (py-6 на trigger вместо дефолтного py-3), title text-17
// font-semibold. Indicator оставлен chevron (наш системный default) —
// figma использует +/− textual, но это специфика FAQ-секции; за пределами
// этой страницы chevron консистентнее.
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/shared/ui';

import { FAQ_ITEMS, type FaqItem } from '../content';

export interface FAQAccordionProps {
  items?: FaqItem[];
}

export function FAQAccordion({ items = FAQ_ITEMS }: FAQAccordionProps): JSX.Element {
  return (
    <section id="faq" aria-labelledby="faq-title" className="bg-bg px-4 py-16 sm:py-20 lg:px-20">
      <div className="mx-auto flex w-full max-w-[1080px] flex-col items-center gap-12">
        <header className="flex flex-col items-center gap-4 text-center">
          <p className="text-14 font-semibold tracking-[2px] text-brand-500">FAQ</p>
          <h2
            id="faq-title"
            className="text-3xl font-bold leading-[1.1] tracking-[-0.5px] text-fg sm:text-4xl md:text-[44px] md:tracking-[-1px]"
          >
            Часто задаваемые вопросы
          </h2>
        </header>

        <Accordion type="single" collapsible className="w-full">
          {items.map((item) => (
            <AccordionItem key={item.id} value={item.id} className="border-b border-divider">
              <AccordionTrigger className="py-6 text-[17px]">{item.question}</AccordionTrigger>
              <AccordionContent className="text-16 leading-[26px] text-fg-muted">
                {item.answer}
              </AccordionContent>
            </AccordionItem>
          ))}
        </Accordion>
      </div>
    </section>
  );
}
