// @vitest-environment jsdom
//
// QuickStart (ContractDetail) — проверяем 4.7-review-фиксы: недоступные при
// не-READY действия рендерятся как реальные disabled-кнопки (в a11y-дереве),
// а экспорт/шаринг скрыты для ролей без права экспорта (useCanExport).
import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';

import { type User, useSession } from '@/shared/auth';

import { QuickStart } from './quick-start';

const lawyer: User = {
  user_id: '1',
  email: 'l@e.com',
  name: 'Юрист',
  role: 'LAWYER',
  organization_id: '2',
  organization_name: 'Орг',
  permissions: { export_enabled: true },
};
const businessNoExport: User = {
  ...lawyer,
  role: 'BUSINESS_USER',
  permissions: { export_enabled: false },
};

function setup(user: User, props: Parameters<typeof QuickStart>[0]): void {
  useSession.getState().setUser(user);
  render(
    <MemoryRouter>
      <QuickStart {...props} />
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
  useSession.getState().clear();
});

describe('QuickStart (ContractDetail)', () => {
  it('не-READY: «Открыть результат проверки» — реальная disabled-кнопка', () => {
    setup(lawyer, { contractId: 'c1', versionId: 'v1', isReady: false });
    const btn = screen.getByRole('button', { name: /открыть результат проверки/i });
    expect((btn as HTMLButtonElement).disabled).toBe(true);
  });

  it('LAWYER видит действия экспорта', () => {
    setup(lawyer, { contractId: 'c1', versionId: 'v1', isReady: true });
    expect(screen.getByText(/скачать последний отчёт/i)).toBeDefined();
    expect(screen.getByText(/поделиться ссылкой/i)).toBeDefined();
  });

  it('BUSINESS_USER без export_enabled не видит действий экспорта', () => {
    setup(businessNoExport, { contractId: 'c1', versionId: 'v1', isReady: true });
    expect(screen.queryByText(/скачать последний отчёт/i)).toBeNull();
    expect(screen.queryByText(/поделиться ссылкой/i)).toBeNull();
    // Навигационные действия остаются
    expect(screen.getByText(/сравнить версии/i)).toBeDefined();
  });
});
