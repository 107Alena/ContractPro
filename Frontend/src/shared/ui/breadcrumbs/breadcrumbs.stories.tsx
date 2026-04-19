import type { Meta, StoryObj } from '@storybook/react';

import {
  type BreadcrumbItem,
  Breadcrumbs,
  BreadcrumbsItem,
  BreadcrumbsLink,
  BreadcrumbsList,
  BreadcrumbsPage,
  BreadcrumbsRoot,
  BreadcrumbsSeparator,
} from './breadcrumbs';

const meta = {
  title: 'Shared/Breadcrumbs',
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
} satisfies Meta<typeof Breadcrumbs>;
export default meta;

type Story = StoryObj<typeof meta>;

const baseItems: BreadcrumbItem[] = [
  { label: 'Главная', href: '/' },
  { label: 'Документы', href: '/contracts' },
  { label: 'Договор №1289' },
];

export const Default: Story = {
  render: () => <Breadcrumbs items={baseItems} />,
};

export const CustomSeparator: Story = {
  render: () => <Breadcrumbs items={baseItems} separator={<span aria-hidden="true">›</span>} />,
};

export const WithIcons: Story = {
  render: () => (
    <Breadcrumbs
      items={[
        {
          label: 'Главная',
          href: '/',
          icon: (
            <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
              <path
                d="M2 8l6-6 6 6M4 7v6h8V7"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinejoin="round"
              />
            </svg>
          ),
        },
        { label: 'Документы', href: '/contracts' },
        { label: 'Договор №1289' },
      ]}
    />
  ),
};

export const Collapsed: Story = {
  name: 'Collapsed (maxItems=3)',
  render: () => (
    <Breadcrumbs
      items={[
        { label: 'Организация', href: '/org' },
        { label: 'Проекты', href: '/org/projects' },
        { label: 'Проект «Альфа»', href: '/org/projects/alpha' },
        { label: 'Документы', href: '/org/projects/alpha/docs' },
        { label: 'Договор №1289' },
      ]}
      maxItems={3}
    />
  ),
};

export const SingleItem: Story = {
  render: () => <Breadcrumbs items={[{ label: 'Только одна' }]} />,
};

export const SizeSmall: Story = {
  name: 'Size: sm',
  render: () => <Breadcrumbs items={baseItems} size="sm" />,
};

export const CompoundApiCustomLink: Story = {
  name: 'Compound API (custom link)',
  render: () => (
    <BreadcrumbsRoot label="Custom compound">
      <BreadcrumbsList>
        <BreadcrumbsItem>
          <BreadcrumbsLink asChild>
            <a href="/" data-custom-router="true">
              Главная (кастомная ссылка)
            </a>
          </BreadcrumbsLink>
        </BreadcrumbsItem>
        <BreadcrumbsSeparator />
        <BreadcrumbsItem>
          <BreadcrumbsLink href="/contracts">Документы</BreadcrumbsLink>
        </BreadcrumbsItem>
        <BreadcrumbsSeparator />
        <BreadcrumbsItem>
          <BreadcrumbsPage>Договор №1289</BreadcrumbsPage>
        </BreadcrumbsItem>
      </BreadcrumbsList>
    </BreadcrumbsRoot>
  ),
};
