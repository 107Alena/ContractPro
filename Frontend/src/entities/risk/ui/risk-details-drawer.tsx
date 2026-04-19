// RiskDetailsDrawer — детальная карточка риска (§16.5 компонентное дерево
// ResultPage, §17.5 artifact RISK_ANALYSIS). Открывается по клику на
// карточку в RisksList (§8.3 «RiskBadge + legend»).
//
// Реализация на Radix Dialog (alias Modal) — совпадает со стилевой базой
// (§8.3). «Drawer» здесь — семантика (правая панель с подробностями), но
// технически это модальное окно с фокус-трапом, что достаточно для v1.
//
// Page-local state управляет `open` — drawer controlled, risk-entity
// ничего не хранит про себя.
import { type ReactElement, useId } from 'react';

import { Button } from '@/shared/ui/button';
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalDescription,
  ModalFooter,
  ModalHeader,
  ModalTitle,
} from '@/shared/ui/modal';

import type { RiskLevel } from '../model';
import { RISK_LEVEL_META } from '../model';
import { RiskBadge } from './risk-badge';

export interface RiskDetailsDrawerRisk {
  id?: string | undefined;
  level?: RiskLevel | undefined;
  description?: string | undefined;
  clause_ref?: string | undefined;
  legal_basis?: string | undefined;
}

export interface RiskDetailsDrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  risk: RiskDetailsDrawerRisk | null;
}

export function RiskDetailsDrawer({
  open,
  onOpenChange,
  risk,
}: RiskDetailsDrawerProps): ReactElement {
  const level = risk?.level;
  const meta = level ? RISK_LEVEL_META[level] : undefined;
  const descriptionId = useId();

  return (
    <Modal open={open} onOpenChange={onOpenChange}>
      <ModalContent size="lg" data-testid="risk-details-drawer" aria-describedby={descriptionId}>
        <ModalHeader>
          <div className="flex items-center gap-2">
            {level ? <RiskBadge level={level} /> : null}
            <ModalTitle>Детали риска</ModalTitle>
          </div>
          <ModalDescription id={descriptionId}>
            {meta?.legend ?? 'Полная информация о выявленном риске и применимой норме.'}
          </ModalDescription>
        </ModalHeader>

        <ModalBody>
          {!risk ? (
            <p className="text-sm text-fg-muted">Риск не выбран.</p>
          ) : (
            <dl className="flex flex-col gap-4">
              {risk.description ? (
                <div className="flex flex-col gap-1">
                  <dt className="text-xs font-medium uppercase tracking-wide text-fg-muted">
                    Описание
                  </dt>
                  <dd className="text-sm text-fg" data-testid="risk-details-description-value">
                    {risk.description}
                  </dd>
                </div>
              ) : null}
              {risk.clause_ref ? (
                <div className="flex flex-col gap-1">
                  <dt className="text-xs font-medium uppercase tracking-wide text-fg-muted">
                    Пункт договора
                  </dt>
                  <dd className="text-sm text-fg" data-testid="risk-details-clause">
                    {risk.clause_ref}
                  </dd>
                </div>
              ) : null}
              {risk.legal_basis ? (
                <div className="flex flex-col gap-1">
                  <dt className="text-xs font-medium uppercase tracking-wide text-fg-muted">
                    Правовое основание
                  </dt>
                  <dd className="text-sm text-fg" data-testid="risk-details-legal-basis">
                    {risk.legal_basis}
                  </dd>
                </div>
              ) : null}
              {!risk.description && !risk.clause_ref && !risk.legal_basis ? (
                <p className="text-sm text-fg-muted">Для этого риска подробности отсутствуют.</p>
              ) : null}
            </dl>
          )}
        </ModalBody>

        <ModalFooter>
          <Button
            type="button"
            variant="primary"
            onClick={() => onOpenChange(false)}
            data-testid="risk-details-close"
          >
            Закрыть
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
