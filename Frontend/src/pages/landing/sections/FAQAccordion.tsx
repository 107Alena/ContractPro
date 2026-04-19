// FAQAccordion — секция «Часто задаваемые вопросы» на базе shared/ui Accordion
// (Radix Accordion, §17.4). type="single" collapsible — одна открытая панель
// одновременно, по умолчанию все закрыты.
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/shared/ui';

import { FAQ_ITEMS, type FaqItem } from '../content';

export interface FAQAccordionProps {
  items?: FaqItem[];
}

export function FAQAccordion({ items = FAQ_ITEMS }: FAQAccordionProps): JSX.Element {
  return (
    <section id="faq" aria-labelledby="faq-title" className="bg-bg py-16 md:py-20 lg:py-24">
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-8 px-4">
        <header className="flex flex-col gap-3 text-center">
          <p className="text-sm font-semibold uppercase tracking-wider text-brand-600">FAQ</p>
          <h2 id="faq-title" className="text-2xl font-semibold text-fg md:text-3xl">
            Часто задаваемые вопросы
          </h2>
        </header>

        <Accordion type="single" collapsible className="w-full">
          {items.map((item) => (
            <AccordionItem key={item.id} value={item.id}>
              <AccordionTrigger>{item.question}</AccordionTrigger>
              <AccordionContent>{item.answer}</AccordionContent>
            </AccordionItem>
          ))}
        </Accordion>
      </div>
    </section>
  );
}
