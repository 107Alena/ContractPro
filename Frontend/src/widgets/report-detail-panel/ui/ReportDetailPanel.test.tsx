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

  it('Share — LAWYER с резолвнутым versionId может кликнуть, callback с UUID', () => {
    const onOpenShare = vi.fn();
    renderPanel({ contract, versionId: 'ver-uuid-1', onOpenShare });
    const btn = screen.getByTestId('report-detail-panel-share');
    expect(btn).not.toBeDisabled();
    fireEvent.click(btn);
    expect(onOpenShare).toHaveBeenCalledWith({ contractId: 'c1', versionId: 'ver-uuid-1' });
  });

  it('Share — без versionId (детали ещё грузятся) → кнопка disabled даже у LAWYER', () => {
    const onOpenShare = vi.fn();
    renderPanel({ contract, onOpenShare });
    expect(screen.getByTestId('report-detail-panel-share')).toBeDisabled();
  });

  it('Share — BUSINESS_USER без экспорта видит disabled-кнопку', () => {
    const onOpenShare = vi.fn();
    renderPanel({ contract, onOpenShare }, businessUserNoExport);
    const btn = screen.getByTestId('report-detail-panel-share');
    expect(btn).toBeDisabled();
  });

  it('Risk — showRisk + профиль → уровень и распределение', () => {
    renderPanel({
      contract,
      showRisk: true,
      riskProfile: { level: 'medium', high: 2, medium: 3, low: 1 },
    });
    expect(screen.getByTestId('report-detail-risk')).toBeInTheDocument();
    expect(screen.getByTestId('report-detail-risk-level')).toHaveTextContent('Средний риск');
    expect(screen.getByText('высоких')).toBeInTheDocument();
    expect(screen.getByText('средних')).toBeInTheDocument();
    expect(screen.getByText('низких')).toBeInTheDocument();
  });

  it('Risk — профиль без вердикта (level=null) → нейтральный заголовок + счётчики, без выдумки', () => {
    renderPanel({
      contract,
      showRisk: true,
      riskProfile: { level: null, high: 1, medium: 5, low: 9 },
    });
    expect(screen.getByTestId('report-detail-risk-level')).toHaveTextContent('Профиль рисков');
    expect(screen.getByText('высоких')).toBeInTheDocument();
  });

  it('Risk — showRisk + loading → спиннер', () => {
    renderPanel({ contract, showRisk: true, riskLoading: true });
    expect(screen.getByTestId('report-detail-risk-loading')).toBeInTheDocument();
  });

  it('Risk — showRisk без профиля (нет артефакта) → честный плейсхолдер', () => {
    renderPanel({ contract, showRisk: true, riskProfile: null });
    expect(screen.getByTestId('report-detail-risk-empty')).toBeInTheDocument();
  });

  it('Risk — нет права risks.view (showRisk=false) → риск-секция не рендерится', () => {
    renderPanel({
      contract,
      showRisk: false,
      riskProfile: { level: 'high', high: 1, medium: 0, low: 0 },
    });
    expect(screen.queryByTestId('report-detail-risk')).toBeNull();
  });
});
