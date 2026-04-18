// Фикстуры admin: политики и чек-листы организации (OPM).

import type { components } from '@/shared/api/openapi';

import { IDS } from './ids';

type Policy = components['schemas']['Policy'];
type Checklist = components['schemas']['Checklist'];

export const defaultPolicy: Policy = {
  policy_id: IDS.policies.default,
  name: 'Базовая политика проверки договоров',
  description: 'Минимальный набор обязательных проверок для всех типов договоров.',
  settings: {},
};

export const servicesChecklist: Checklist = {
  checklist_id: IDS.checklists.services,
  contract_type: 'услуги',
  items: [
    { id: 'item-1', name: 'Указан предмет договора', enabled: true, severity: 'high' },
    { id: 'item-2', name: 'Определена цена и порядок расчётов', enabled: true, severity: 'high' },
    { id: 'item-3', name: 'Установлены сроки оказания услуг', enabled: true, severity: 'medium' },
    { id: 'item-4', name: 'Порядок приёмки услуг', enabled: true, severity: 'medium' },
    { id: 'item-5', name: 'Ответственность сторон', enabled: true, severity: 'high' },
    { id: 'item-6', name: 'Порядок разрешения споров', enabled: false, severity: 'low' },
  ],
};

export const policies: Policy[] = [defaultPolicy];
export const checklists: Checklist[] = [servicesChecklist];
