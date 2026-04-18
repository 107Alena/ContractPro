// Фикстуры пользователей для MSW-handlers (§5.5 RBAC).
// Каждая роль — отдельный fixture; тесты и Storybook выбирают нужного через
// server.use(getCurrentUserHandler(fixtures.users.lawyer)).

import type { components } from '@/shared/api/openapi';

import { IDS } from './ids';

type UserProfile = components['schemas']['UserProfile'];

const organizationId = IDS.organization;
const organizationName = 'ООО «Контракт-Сервис»';

export const lawyer: UserProfile = {
  user_id: IDS.users.lawyer,
  email: 'lawyer@contractpro.local',
  name: 'Алина Юрьева',
  role: 'LAWYER',
  organization_id: organizationId,
  organization_name: organizationName,
  permissions: { export_enabled: true },
};

export const businessUser: UserProfile = {
  user_id: IDS.users.businessUser,
  email: 'business@contractpro.local',
  name: 'Игорь Бизнесов',
  role: 'BUSINESS_USER',
  organization_id: organizationId,
  organization_name: organizationName,
  permissions: { export_enabled: false },
};

export const orgAdmin: UserProfile = {
  user_id: IDS.users.orgAdmin,
  email: 'admin@contractpro.local',
  name: 'Ольга Админ',
  role: 'ORG_ADMIN',
  organization_id: organizationId,
  organization_name: organizationName,
  permissions: { export_enabled: true },
};

export const users = { lawyer, businessUser, orgAdmin };
