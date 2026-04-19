import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it } from 'vitest';

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  accordionItemVariants,
  AccordionTrigger,
  accordionTriggerVariants,
} from './accordion';

afterEach(() => cleanup());

describe('accordionItemVariants', () => {
  it('default variant bordered → border-b', () => {
    expect(accordionItemVariants({})).toContain('border-b');
  });

  it('variant ghost has no border', () => {
    expect(accordionItemVariants({ variant: 'ghost' })).not.toContain('border-b');
  });
});

describe('accordionTriggerVariants', () => {
  it('size sm → py-2', () => {
    expect(accordionTriggerVariants({ size: 'sm' })).toContain('py-2');
  });

  it('default has chevron rotation rule', () => {
    expect(accordionTriggerVariants({})).toContain('[&[data-state=open]>svg]:rotate-180');
  });
});

describe('Accordion (integration)', () => {
  function renderSingle() {
    return render(
      <Accordion type="single" collapsible defaultValue="a">
        <AccordionItem value="a">
          <AccordionTrigger>Заголовок A</AccordionTrigger>
          <AccordionContent>Контент A</AccordionContent>
        </AccordionItem>
        <AccordionItem value="b">
          <AccordionTrigger>Заголовок B</AccordionTrigger>
          <AccordionContent>Контент B</AccordionContent>
        </AccordionItem>
      </Accordion>,
    );
  }

  it('renders items and initially open content', () => {
    renderSingle();
    expect(screen.getByText('Заголовок A')).toBeInTheDocument();
    expect(screen.getByText('Контент A')).toBeInTheDocument();
  });

  it('toggles content on trigger click (single collapsible)', async () => {
    const user = userEvent.setup();
    renderSingle();
    const trigB = screen.getByRole('button', { name: 'Заголовок B' });
    expect(trigB).toHaveAttribute('aria-expanded', 'false');
    await user.click(trigB);
    expect(trigB).toHaveAttribute('aria-expanded', 'true');
  });

  it('collapses when reopened (collapsible)', async () => {
    const user = userEvent.setup();
    renderSingle();
    const trigA = screen.getByRole('button', { name: 'Заголовок A' });
    expect(trigA).toHaveAttribute('aria-expanded', 'true');
    await user.click(trigA);
    expect(trigA).toHaveAttribute('aria-expanded', 'false');
  });

  it('type="multiple" allows multiple open items', async () => {
    const user = userEvent.setup();
    render(
      <Accordion type="multiple" defaultValue={['a']}>
        <AccordionItem value="a">
          <AccordionTrigger>A</AccordionTrigger>
          <AccordionContent>Content A</AccordionContent>
        </AccordionItem>
        <AccordionItem value="b">
          <AccordionTrigger>B</AccordionTrigger>
          <AccordionContent>Content B</AccordionContent>
        </AccordionItem>
      </Accordion>,
    );
    const trigB = screen.getByRole('button', { name: 'B' });
    await user.click(trigB);
    // оба открыты
    expect(screen.getByRole('button', { name: 'A' })).toHaveAttribute('aria-expanded', 'true');
    expect(trigB).toHaveAttribute('aria-expanded', 'true');
  });

  it('keyboard: ArrowDown moves focus to next trigger', async () => {
    const user = userEvent.setup();
    renderSingle();
    const trigA = screen.getByRole('button', { name: 'Заголовок A' });
    trigA.focus();
    await user.keyboard('{ArrowDown}');
    expect(screen.getByRole('button', { name: 'Заголовок B' })).toHaveFocus();
  });

  it('disabled trigger cannot be activated', async () => {
    const user = userEvent.setup();
    render(
      <Accordion type="single" collapsible>
        <AccordionItem value="a">
          <AccordionTrigger disabled>Заблокировано</AccordionTrigger>
          <AccordionContent>Never</AccordionContent>
        </AccordionItem>
      </Accordion>,
    );
    const trig = screen.getByRole('button', { name: 'Заблокировано' });
    expect(trig).toBeDisabled();
    await user.click(trig);
    expect(trig).toHaveAttribute('aria-expanded', 'false');
  });

  it('hides chevron when hideChevron=true', () => {
    const { container } = render(
      <Accordion type="single" collapsible>
        <AccordionItem value="a">
          <AccordionTrigger hideChevron>Нет шеврона</AccordionTrigger>
          <AccordionContent>X</AccordionContent>
        </AccordionItem>
      </Accordion>,
    );
    const svgs = container.querySelectorAll('svg');
    expect(svgs.length).toBe(0);
  });
});
