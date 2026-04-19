import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';

import { Button } from '@/shared/ui/button';

import { EmptyState, emptyStateVariants } from './empty-state';

afterEach(() => cleanup());

describe('emptyStateVariants', () => {
  it('default size md → p-10', () => {
    expect(emptyStateVariants({})).toContain('p-10');
  });

  it('size sm → p-6', () => {
    expect(emptyStateVariants({ size: 'sm' })).toContain('p-6');
  });

  it('size lg → p-14', () => {
    expect(emptyStateVariants({ size: 'lg' })).toContain('p-14');
  });

  it('tone subtle removes border+bg', () => {
    expect(emptyStateVariants({ tone: 'subtle' })).toContain('border-transparent');
    expect(emptyStateVariants({ tone: 'subtle' })).toContain('bg-transparent');
  });
});

describe('EmptyState', () => {
  it('renders title and description', () => {
    render(<EmptyState title="Нет документов" description="Загрузите первый договор" />);
    expect(screen.getByText('Нет документов')).toBeInTheDocument();
    expect(screen.getByText('Загрузите первый договор')).toBeInTheDocument();
  });

  it('exposes role="status" and aria-live=polite by default', () => {
    render(<EmptyState title="Empty" />);
    const region = screen.getByRole('status');
    expect(region).toHaveAttribute('aria-live', 'polite');
  });

  it('links title via aria-labelledby', () => {
    render(<EmptyState title="Пусто" description="desc" />);
    const region = screen.getByRole('status');
    const labelledBy = region.getAttribute('aria-labelledby');
    expect(labelledBy).toBeTruthy();
    expect(document.getElementById(labelledBy!)).toHaveTextContent('Пусто');
  });

  it('renders icon with aria-hidden', () => {
    render(
      <EmptyState
        title="Пусто"
        icon={
          <svg data-testid="icon" width="24" height="24">
            <circle cx="12" cy="12" r="6" />
          </svg>
        }
      />,
    );
    const icon = screen.getByTestId('icon');
    expect(icon.parentElement).toHaveAttribute('aria-hidden', 'true');
  });

  it('renders action CTA button', () => {
    render(<EmptyState title="Пусто" action={<Button>Загрузить</Button>} />);
    expect(screen.getByRole('button', { name: 'Загрузить' })).toBeInTheDocument();
  });

  it('renders both action and secondaryAction', () => {
    render(
      <EmptyState
        title="Пусто"
        action={<Button>Загрузить</Button>}
        secondaryAction={<Button variant="ghost">Обновить</Button>}
      />,
    );
    expect(screen.getByRole('button', { name: 'Загрузить' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Обновить' })).toBeInTheDocument();
  });

  it('supports role="alert" for error presentation', () => {
    render(<EmptyState title="Ошибка" role="alert" />);
    const alert = screen.getByRole('alert');
    expect(alert).toBeInTheDocument();
    expect(alert).not.toHaveAttribute('aria-live');
  });

  it('forwards ref to root element', () => {
    let captured: HTMLDivElement | null = null;
    render(
      <EmptyState
        title="Пусто"
        ref={(el) => {
          captured = el;
        }}
      />,
    );
    expect(captured).toBeInstanceOf(HTMLDivElement);
  });

  it('merges className', () => {
    render(<EmptyState title="Пусто" className="custom-cls" data-testid="root" />);
    expect(screen.getByTestId('root')).toHaveClass('custom-cls');
  });

  it('default heading level is h2', () => {
    render(<EmptyState title="Пусто" />);
    const h = screen.getByText('Пусто');
    expect(h.tagName.toLowerCase()).toBe('h2');
  });

  it('respects headingLevel=h3', () => {
    render(<EmptyState title="Нет данных" headingLevel="h3" />);
    const h = screen.getByText('Нет данных');
    expect(h.tagName.toLowerCase()).toBe('h3');
  });
});
