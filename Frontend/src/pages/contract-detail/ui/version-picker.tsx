// VersionPicker — селект для выбора версии и перехода к её результату.
// На карточке договора (экран 8 Figma, §17.4): позволяет быстро прыгнуть
// к /contracts/:id/versions/:vid/result без возврата в историю.
//
// В v1 — нативный <select> (доступный, простой). Shared Popover/Listbox
// пока нет; при необходимости заменим на Radix Popover без API-ломки
// (controlled-паттерн уже соблюдён).
import { useCallback } from 'react';
import { useNavigate } from 'react-router-dom';

import type { VersionDetails } from '@/entities/version';
import { Label } from '@/shared/ui';

export interface VersionPickerProps {
  contractId: string;
  versions: readonly VersionDetails[];
  selectedVersionId?: string | undefined;
}

export function VersionPicker({
  contractId,
  versions,
  selectedVersionId,
}: VersionPickerProps): JSX.Element {
  const navigate = useNavigate();

  const onChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const vid = e.target.value;
      if (!vid) return;
      navigate(`/contracts/${contractId}/versions/${vid}/result`);
    },
    [contractId, navigate],
  );

  if (versions.length === 0) {
    return (
      <section
        aria-label="Выбор версии"
        className="flex flex-col gap-2 rounded-md border border-border bg-bg p-4 shadow-sm"
      >
        <p className="text-sm text-fg-muted">Версий пока нет.</p>
      </section>
    );
  }

  return (
    <section
      aria-label="Выбор версии"
      className="flex flex-col gap-2 rounded-md border border-border bg-bg p-4 shadow-sm"
    >
      <Label htmlFor="version-picker" className="text-xs uppercase tracking-wide text-fg-muted">
        Перейти к версии
      </Label>
      <select
        id="version-picker"
        data-testid="version-picker"
        value={selectedVersionId ?? ''}
        onChange={onChange}
        className="rounded-md border border-border bg-bg px-3 py-2 text-sm text-fg focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
      >
        <option value="" disabled>
          Выберите версию…
        </option>
        {[...versions]
          .sort((a, b) => (b.version_number ?? 0) - (a.version_number ?? 0))
          .map((v) =>
            v.version_id ? (
              <option key={v.version_id} value={v.version_id}>
                v{v.version_number ?? '—'}
                {v.source_file_name ? ` · ${v.source_file_name}` : ''}
              </option>
            ) : null,
          )}
      </select>
    </section>
  );
}
