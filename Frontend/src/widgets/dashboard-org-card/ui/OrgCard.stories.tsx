import type { Meta, StoryObj } from '@storybook/react';

import type { UserProfile } from '@/entities/user';

import { OrgCard } from './OrgCard';

const meta: Meta<typeof OrgCard> = {
  title: 'Widgets/Dashboard/OrgCard',
  component: OrgCard,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof OrgCard>;

const user: UserProfile = {
  user_id: '00000000-0000-0000-0000-000000000abc',
  email: 'maria@company.ru',
  name: 'Мария Петрова',
  role: 'LAWYER',
  organization_id: '00000000-0000-0000-0000-00000000000d',
  organization_name: 'ООО «Правовой центр»',
  permissions: { export_enabled: true },
};

export const Default: Story = { args: { user } };

export const OrgAdmin: Story = {
  args: { user: { ...user, role: 'ORG_ADMIN' } },
};

export const BusinessUser: Story = {
  args: { user: { ...user, role: 'BUSINESS_USER', permissions: { export_enabled: false } } },
};

export const Loading: Story = { args: { isLoading: true } };

export const ErrorState: Story = { args: { error: new Error('network') } };
