import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import {
  type BreadcrumbItem,
  Breadcrumbs,
  breadcrumbsLinkVariants,
  BreadcrumbsList,
  BreadcrumbsPage,
  breadcrumbsPageVariants,
  BreadcrumbsRoot,
  BreadcrumbsSeparator,
  breadcrumbsVariants,
} from './breadcrumbs';

afterEach(() => cleanup());

describe('breadcrumbsVariants', () => {
  it('default size md → text-sm', () => {
    expect(breadcrumbsVariants({})).toContain('text-sm');
  });

  it('size sm → text-xs', () => {
    expect(breadcrumbsVariants({ size: 'sm' })).toContain('text-xs');
  });
});

describe('breadcrumbsLinkVariants / breadcrumbsPageVariants', () => {
  it('link has hover underline', () => {
    expect(breadcrumbsLinkVariants()).toContain('hover:underline');
  });

  it('page has font-medium', () => {
    expect(breadcrumbsPageVariants()).toContain('font-medium');
  });
});

describe('Breadcrumbs (flat)', () => {
  const items: BreadcrumbItem[] = [
    { label: 'Главная', href: '/' },
    { label: 'Документы', href: '/contracts' },
    { label: 'Договор #42' },
  ];

  it('рендерит nav с aria-label', () => {
    render(<Breadcrumbs items={items} />);
    expect(screen.getByRole('navigation', { name: 'Хлебные крошки' })).toBeInTheDocument();
  });

  it('последний элемент — aria-current="page"', () => {
    render(<Breadcrumbs items={items} />);
    const current = screen.getByText('Договор #42');
    expect(current).toHaveAttribute('aria-current', 'page');
  });

  it('элементы с href рендерятся как ссылки', () => {
    render(<Breadcrumbs items={items} />);
    expect(screen.getByRole('link', { name: 'Главная' })).toHaveAttribute('href', '/');
    expect(screen.getByRole('link', { name: 'Документы' })).toHaveAttribute('href', '/contracts');
  });

  it('рендерит разделители между элементами', () => {
    const { container } = render(<Breadcrumbs items={items} />);
    const separators = container.querySelectorAll('[role="presentation"]');
    expect(separators.length).toBe(items.length - 1);
  });

  it('кастомный separator', () => {
    render(<Breadcrumbs items={items} separator={<span data-testid="sep">→</span>} />);
    expect(screen.getAllByTestId('sep').length).toBe(2);
  });

  it('поддерживает кастомный aria-label', () => {
    render(<Breadcrumbs items={items} label="Навигация" />);
    expect(screen.getByRole('navigation', { name: 'Навигация' })).toBeInTheDocument();
  });

  it('collapse через maxItems', () => {
    const many: BreadcrumbItem[] = [
      { label: 'L0', href: '/' },
      { label: 'L1', href: '/1' },
      { label: 'L2', href: '/1/2' },
      { label: 'L3', href: '/1/2/3' },
      { label: 'L4' },
    ];
    render(<Breadcrumbs items={many} maxItems={3} />);
    expect(screen.getByText('…')).toBeInTheDocument();
    expect(screen.getByText('L0')).toBeInTheDocument();
    expect(screen.getByText('L4')).toBeInTheDocument();
    expect(screen.queryByText('L2')).not.toBeInTheDocument();
  });

  it('без href рендерится как текущая страница', () => {
    const it: BreadcrumbItem[] = [{ label: 'Только текст' }];
    render(<Breadcrumbs items={it} />);
    const only = screen.getByText('Только текст');
    expect(only.tagName.toLowerCase()).toBe('span');
    expect(only).toHaveAttribute('aria-current', 'page');
  });

  it('current=true помечает элемент как текущий', () => {
    const it: BreadcrumbItem[] = [
      { label: 'A', href: '/a', current: true },
      { label: 'B', href: '/b' },
    ];
    render(<Breadcrumbs items={it} />);
    expect(screen.getByText('A')).toHaveAttribute('aria-current', 'page');
  });

  it('промежуточный элемент без href не получает aria-current (только последний)', () => {
    const many: BreadcrumbItem[] = [
      { label: 'Главная', href: '/' },
      { label: 'Без ссылки (промежуточный)' },
      { label: 'Текущий' },
    ];
    render(<Breadcrumbs items={many} />);
    const middle = screen.getByText('Без ссылки (промежуточный)');
    const last = screen.getByText('Текущий');
    expect(middle).not.toHaveAttribute('aria-current');
    expect(last).toHaveAttribute('aria-current', 'page');
  });
});

describe('Breadcrumbs (compound API)', () => {
  it('compound-примитивы экспортируются и работают', () => {
    render(
      <BreadcrumbsRoot label="Custom">
        <BreadcrumbsList>
          <li>
            <a href="/">Главная</a>
          </li>
          <BreadcrumbsSeparator />
          <li>
            <BreadcrumbsPage>Текущая</BreadcrumbsPage>
          </li>
        </BreadcrumbsList>
      </BreadcrumbsRoot>,
    );
    expect(screen.getByRole('navigation', { name: 'Custom' })).toBeInTheDocument();
    expect(screen.getByText('Текущая')).toHaveAttribute('aria-current', 'page');
  });
});
