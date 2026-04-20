// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { ContractSummary } from '@/entities/contract';
import { type User, useSession } from '@/shared/auth';

import { ReportDetailPanel } from './ReportDetailPanel';

const lawyer: User = {
  user_id: '00000000-0000-0000-0000-000000000001',
  email: 'maria@example.com',
  name: 'Мария Петрова',
  role: 'LAWYER',
  organization_id: '00000000-0000-0000-0000-000000000002',
  organization_name: 'ООО «Правовой центр»',
  permissions: { export_enabled: true },
};
const businessUserNoExport: User = {
  ...lawyer,
  role: 'BUSINESS_USER',
  permissions: { export_enabled: false },
};

const contract: ContractSummary = {
  contract_id: 'c1',
  title: 'Договор услуг',
  status: 'ACTIVE',
  current_version_number: 2,
  processing_status: 'READY',
  updated_at: '2026-04-19T14:20:00Z',
};

function renderPanel(
  props: Partial<React.ComponentProps<typeof ReportDetailPanel>>,
  user: User | null = lawyer,
): void {
  if (user) useSession.getState().setUser(user);
  else useSession.getState().clear();
  render(
    <MemoryRouter>
      <ReportDetailPanel contract={null} onClose={() => {}} {...props} />
    </MemoryRouter>,
  );
}

afterEach(() => {
  cleanup();
  useSession.getState().clear();
});

describe('ReportDetailPanel', () => {
  it('contract=null → не рендерится', () => {
    renderPanel({ contract: null });
    expect(screen.queryByTestId('report-detail-panel')).toBeNull();
  });

  it('Open — title + version + статус рендерятся', () => {
    renderPanel({ contract });
    expect(screen.getByTestId('report-detail-panel')).toBeInTheDocument();
    expect(screen.getByText('Договор услуг')).toBeInTheDocument();
    expect(screen.getByText('v2')).toBeInTheDocument();
  });

  it('Link «Открыть результаты» ведёт на /contracts/:id', () => {
    renderPanel({ contract });
    const link = screen.getByTestId('report-detail-panel-open');
    expect(link).toHaveAttribute('href', '/contracts/c1');
  });

  it('Close — клик вызывает onClose', () => {
    const onClose = vi.fn();
    renderPanel({ contract, onClose });
    fireEvent.click(screen.getByTestId('report-detail-panel-close'));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('Share — LAWYER может кликнуть, callback вызывается', () => {
    const onOpenShare = vi.fn();
    renderPanel({ contract, onOpenShare });
    const btn = screen.getByTestId('report-detail-panel-share');
    expect(btn).not.toBeDisabled();
    fireEvent.click(btn);
    expect(onOpenShare).toHaveBeenCalledWith({ contractId: 'c1', versionId: '2' });
  });

  it('Share — BUSINESS_USER без экспорта видит disabled-кнопку', () => {
    const onOpenShare = vi.fn();
    renderPanel({ contract, onOpenShare }, businessUserNoExport);
    const btn = screen.getByTestId('report-detail-panel-share');
    expect(btn).toBeDisabled();
  });
});
