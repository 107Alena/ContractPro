import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';

import { Topbar } from './topbar';

interface StoryArgs {
  withSearch: boolean;
  withNotifications: boolean;
  forceOffline: boolean;
}

function TopbarHarness({ withSearch, withNotifications, forceOffline }: StoryArgs): JSX.Element {
  const [query, setQuery] = useState('');

  const topbarProps: Parameters<typeof Topbar>[0] = { withNotifications, forceOffline };
  if (withSearch) {
    topbarProps.search = { value: query, onChange: setQuery };
  }

  return (
    <div className="min-h-[280px] bg-bg-muted">
      <Topbar {...topbarProps} />
      <div className="p-6 text-sm text-fg-muted">
        поиск: {withSearch ? `«${query || '—'}»` : 'скрыт'} · notif:{' '}
        {withNotifications ? 'on' : 'off'} · offline: {forceOffline ? 'yes' : 'no'}
      </div>
    </div>
  );
}

const meta = {
  title: 'Widgets/Topbar',
  component: TopbarHarness,
  parameters: { layout: 'fullscreen' },
  tags: ['autodocs'],
  argTypes: {
    withSearch: { control: 'boolean' },
    withNotifications: { control: 'boolean' },
    forceOffline: { control: 'boolean' },
  },
  args: {
    withSearch: false,
    withNotifications: false,
    forceOffline: false,
  },
} satisfies Meta<typeof TopbarHarness>;

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = { name: 'Default (без поиска)' };

export const WithSearch: Story = {
  name: 'С SearchInput',
  args: { withSearch: true },
};

export const WithNotifications: Story = {
  name: 'С кнопкой уведомлений (placeholder)',
  args: { withNotifications: true },
};

export const Offline: Story = {
  name: 'Offline-баннер (sticky)',
  args: { forceOffline: true },
};
