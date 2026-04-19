import { useId } from 'react';

import { Button } from '@/shared/ui/button';
import { Chip } from '@/shared/ui/chip';
import {
  Modal,
  ModalBody,
  ModalClose,
  ModalContent,
  ModalDescription,
  ModalFooter,
  ModalHeader,
  ModalOverlay,
  ModalPortal,
  ModalTitle,
} from '@/shared/ui/modal';

import type { FilterDefinition, FilterGroupValue } from '../model/types';

export interface MoreFiltersModalProps {
  open: boolean;
  onOpenChange: (next: boolean) => void;
  definitions: readonly FilterDefinition[];
  values: FilterGroupValue;
  onToggleOption: (key: string, value: string) => void;
  onClear: (key?: string) => void;
  title?: string;
}

export function MoreFiltersModal({
  open,
  onOpenChange,
  definitions,
  values,
  onToggleOption,
  onClear,
  title = 'Фильтры',
}: MoreFiltersModalProps) {
  const descId = useId();
  return (
    <Modal open={open} onOpenChange={onOpenChange}>
      <ModalPortal>
        <ModalOverlay />
        <ModalContent size="md" aria-describedby={descId}>
          <ModalHeader>
            <ModalTitle>{title}</ModalTitle>
            <ModalDescription id={descId} className="sr-only">
              Уточнение фильтров для списка документов
            </ModalDescription>
          </ModalHeader>
          <ModalBody>
            <div className="flex flex-col gap-4 max-h-[60vh] overflow-y-auto pr-1">
              {definitions.map((def) => (
                <fieldset key={def.key} className="flex flex-col gap-2">
                  <legend className="text-sm font-medium text-fg">{def.label}</legend>
                  <div className="flex flex-wrap gap-2">
                    {def.options.map((opt) => {
                      const v = values[def.key];
                      const selected =
                        def.kind === 'multi'
                          ? Array.isArray(v) && v.includes(opt.value)
                          : v === opt.value;
                      return (
                        <Chip
                          key={opt.value}
                          selected={selected}
                          interactive
                          onClick={() => onToggleOption(def.key, opt.value)}
                          data-testid={`more-filter-${def.key}-${opt.value}`}
                        >
                          {opt.icon}
                          {opt.label}
                        </Chip>
                      );
                    })}
                  </div>
                </fieldset>
              ))}
            </div>
          </ModalBody>
          <ModalFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onClear()}
              data-testid="more-filter-clear"
            >
              Сбросить всё
            </Button>
            <ModalClose asChild>
              <Button type="button" variant="primary">
                Готово
              </Button>
            </ModalClose>
          </ModalFooter>
        </ModalContent>
      </ModalPortal>
    </Modal>
  );
}
