import type { Meta, StoryObj } from '@storybook/react';

import type { UserProfile } from '@/entities/user';

import { WelcomeBlock } from './WelcomeBlock';

const user: UserProfile = {
  user_id: '00000000-0000-0000-0000-000000000001',
  email: 'maria@company.ru',
  name: 'Мария Петрова',
  role: 'LAWYER',
  organization_id: '00000000-0000-0000-0000-000000000002',
  organization_name: 'ООО «Правовой центр»',
  permissions: { export_enabled: true },
};

const meta: Meta<typeof WelcomeBlock> = {
  title: 'Widgets/Dashboard/WelcomeBlock',
  component: WelcomeBlock,
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof WelcomeBlock>;

export const Default: Story = { args: { user } };

export const NoName: Story = { args: { user: undefined } };
