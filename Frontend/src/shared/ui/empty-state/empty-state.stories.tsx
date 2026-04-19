import type { Meta, StoryObj } from '@storybook/react';

import { Button } from '@/shared/ui/button';

import { EmptyState } from './empty-state';

const FolderIcon = () => (
  <svg aria-hidden="true" focusable="false" viewBox="0 0 48 48" width="48" height="48" fill="none">
    <path
      d="M8 14a4 4 0 0 1 4-4h8l4 4h12a4 4 0 0 1 4 4v16a4 4 0 0 1-4 4H12a4 4 0 0 1-4-4V14Z"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinejoin="round"
    />
  </svg>
);

const meta = {
  title: 'Shared/EmptyState',
  component: EmptyState,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
  args: {
    title: 'У вас пока нет документов',
    description: 'Загрузите первый договор, чтобы начать проверку.',
  },
} satisfies Meta<typeof EmptyState>;
export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const WithIcon: Story = {
  args: {
    icon: <FolderIcon />,
  },
};

export const WithAction: Story = {
  args: {
    icon: <FolderIcon />,
    action: <Button>Загрузить договор</Button>,
  },
};

export const TwoActions: Story = {
  args: {
    icon: <FolderIcon />,
    action: <Button>Загрузить договор</Button>,
    secondaryAction: <Button variant="ghost">Сбросить фильтры</Button>,
  },
};

export const SizeSmall: Story = {
  name: 'Size: sm',
  args: { size: 'sm' },
};

export const SizeLarge: Story = {
  name: 'Size: lg',
  args: { size: 'lg', icon: <FolderIcon /> },
};

export const ToneSubtle: Story = {
  name: 'Tone: subtle (без рамки)',
  args: {
    tone: 'subtle',
    icon: <FolderIcon />,
  },
};

export const AsAlert: Story = {
  name: 'role="alert" (ошибка)',
  args: {
    title: 'Не удалось загрузить список',
    description: 'Проверьте соединение с сервером и попробуйте снова.',
    role: 'alert',
    action: <Button variant="secondary">Повторить</Button>,
  },
};

export const NoDescription: Story = {
  args: {
    description: undefined,
    icon: <FolderIcon />,
  },
};
