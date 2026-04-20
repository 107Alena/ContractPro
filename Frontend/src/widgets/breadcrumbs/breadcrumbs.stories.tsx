import type { Meta, StoryObj } from '@storybook/react';
import type { RouteObject, UIMatch } from 'react-router-dom';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';

import { AppBreadcrumbs } from './breadcrumbs';

interface HarnessArgs {
  path: string;
  maxItems: number;
}

function BreadcrumbsHarness({ path, maxItems }: HarnessArgs): JSX.Element {
  const routes: RouteObject[] = [
    {
      path: '/',
      element: <AppBreadcrumbs maxItems={maxItems} />,
      handle: { crumb: 'Главная' },
      children: [
        {
          path: 'contracts',
          element: <AppBreadcrumbs maxItems={maxItems} />,
          handle: { crumb: 'Документы' },
          children: [
            {
              path: ':id',
              element: <AppBreadcrumbs maxItems={maxItems} />,
              handle: {
                crumb: (m: UIMatch) => `Договор ${(m.params as { id?: string }).id ?? ''}`,
              },
              children: [
                {
                  path: 'versions/:vid/result',
                  element: <AppBreadcrumbs maxItems={maxItems} />,
                  handle: { crumb: 'Результат' },
                },
                {
                  path: 'compare',
                  element: <AppBreadcrumbs maxItems={maxItems} />,
                  handle: { crumb: 'Сравнение версий' },
                },
              ],
            },
          ],
        },
        {
          path: 'admin',
          element: <AppBreadcrumbs maxItems={maxItems} />,
          handle: { crumb: 'Администрирование' },
          children: [
            {
              path: 'policies',
              element: <AppBreadcrumbs maxItems={maxItems} />,
              handle: { crumb: 'Политики' },
            },
          ],
        },
      ],
    },
  ];

  const router = createMemoryRouter(routes, { initialEntries: [path] });
  return <RouterProvider router={router} />;
}

const meta = {
  title: 'Widgets/Breadcrumbs',
  component: BreadcrumbsHarness,
  parameters: { layout: 'padded' },
  tags: ['autodocs'],
  argTypes: {
    path: {
      control: 'select',
      options: [
        '/',
        '/contracts',
        '/contracts/42',
        '/contracts/42/versions/v2/result',
        '/contracts/42/compare',
        '/admin/policies',
      ],
    },
    maxItems: { control: { type: 'number', min: 2, max: 8 } },
  },
  args: {
    path: '/contracts/42',
    maxItems: 5,
  },
} satisfies Meta<typeof BreadcrumbsHarness>;

export default meta;

type Story = StoryObj<typeof meta>;

export const SingleLevel: Story = {
  name: 'Один уровень — только «Главная»',
  args: { path: '/', maxItems: 5 },
};

export const TwoLevels: Story = {
  name: 'Два уровня — Главная / Документы',
  args: { path: '/contracts', maxItems: 5 },
};

export const DynamicCrumb: Story = {
  name: 'Динамический crumb — Договор 42',
  args: { path: '/contracts/42', maxItems: 5 },
};

export const DeepNesting: Story = {
  name: 'Глубокая вложенность — Результат',
  args: { path: '/contracts/42/versions/v2/result', maxItems: 5 },
};

export const Collapsed: Story = {
  name: 'Коллапс середины (maxItems=3)',
  args: { path: '/contracts/42/versions/v2/result', maxItems: 3 },
};
