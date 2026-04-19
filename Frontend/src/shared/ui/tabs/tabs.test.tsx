import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  Tabs,
  TabsContent,
  TabsList,
  tabsListVariants,
  TabsTrigger,
  tabsTriggerVariants,
} from './tabs';

afterEach(() => cleanup());

describe('tabsListVariants', () => {
  it('default variant underline → border-b', () => {
    expect(tabsListVariants({})).toContain('border-b');
  });

  it('variant pills → bg-bg-muted + p-1', () => {
    const cls = tabsListVariants({ variant: 'pills' });
    expect(cls).toContain('bg-bg-muted');
    expect(cls).toContain('p-1');
  });

  it('size sm → h-8', () => {
    expect(tabsListVariants({ size: 'sm' })).toContain('h-8');
  });

  it('fullWidth → w-full', () => {
    expect(tabsListVariants({ fullWidth: true })).toContain('w-full');
  });
});

describe('tabsTriggerVariants', () => {
  it('default variant underline has data-[state=active] styles', () => {
    expect(tabsTriggerVariants({})).toContain('data-[state=active]:border-brand-500');
  });

  it('variant pills has shadow on active', () => {
    expect(tabsTriggerVariants({ variant: 'pills' })).toContain('data-[state=active]:shadow-sm');
  });

  it('fullWidth → flex-1', () => {
    expect(tabsTriggerVariants({ fullWidth: true })).toContain('flex-1');
  });
});

describe('Tabs (integration)', () => {
  function renderTabs(defaultValue = 'a') {
    return render(
      <Tabs defaultValue={defaultValue}>
        <TabsList aria-label="Test tabs">
          <TabsTrigger value="a">Tab A</TabsTrigger>
          <TabsTrigger value="b">Tab B</TabsTrigger>
          <TabsTrigger value="c" disabled>
            Tab C
          </TabsTrigger>
        </TabsList>
        <TabsContent value="a">Content A</TabsContent>
        <TabsContent value="b">Content B</TabsContent>
        <TabsContent value="c">Content C</TabsContent>
      </Tabs>,
    );
  }

  it('renders initial active tab content', () => {
    renderTabs('a');
    expect(screen.getByText('Content A')).toBeInTheDocument();
    expect(screen.queryByText('Content B')).not.toBeInTheDocument();
  });

  it('switches content on tab click', async () => {
    const user = userEvent.setup();
    renderTabs('a');
    await user.click(screen.getByRole('tab', { name: 'Tab B' }));
    expect(screen.getByText('Content B')).toBeInTheDocument();
  });

  it('keyboard: ArrowRight moves focus', async () => {
    const user = userEvent.setup();
    renderTabs('a');
    const tabA = screen.getByRole('tab', { name: 'Tab A' });
    tabA.focus();
    await user.keyboard('{ArrowRight}');
    expect(screen.getByRole('tab', { name: 'Tab B' })).toHaveFocus();
  });

  it('exposes role=tablist / role=tab / role=tabpanel', () => {
    renderTabs();
    expect(screen.getByRole('tablist')).toBeInTheDocument();
    expect(screen.getAllByRole('tab').length).toBe(3);
    expect(screen.getByRole('tabpanel')).toBeInTheDocument();
  });

  it('disabled trigger skipped and unclickable', async () => {
    const user = userEvent.setup();
    const onValueChange = vi.fn();
    render(
      <Tabs defaultValue="a" onValueChange={onValueChange}>
        <TabsList aria-label="t">
          <TabsTrigger value="a">A</TabsTrigger>
          <TabsTrigger value="b" disabled>
            B
          </TabsTrigger>
        </TabsList>
        <TabsContent value="a">A</TabsContent>
        <TabsContent value="b">B</TabsContent>
      </Tabs>,
    );
    const disabled = screen.getByRole('tab', { name: 'B' });
    expect(disabled).toBeDisabled();
    await user.click(disabled);
    expect(onValueChange).not.toHaveBeenCalled();
  });
});
