import type { SVGProps } from 'react';

import { EmptyState } from '@/shared/ui/empty-state';

// Inline-SVG продиктован принципом «placeholder самодостаточен» (FE-TASK-001).
// Дублируется в pages/admin-checklists/AdminChecklistsPage.tsx; при FE-TASK-002
// (полный UI admin) — унести в shared/ui/icons и удалить оба дубля.
function SettingsIcon(props: SVGProps<SVGSVGElement>): JSX.Element {
  return (
    <svg
      width={40}
      height={40}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable={false}
      {...props}
    >
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.6 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.6a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09A1.65 1.65 0 0 0 15 4.6a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9c.14.33.22.7.22 1.09V10a2 2 0 1 1 0 4 1.65 1.65 0 0 0-.22 1z" />
    </svg>
  );
}

export function AdminPoliciesPage(): JSX.Element {
  return (
    <main
      data-testid="page-admin-policies"
      className="mx-auto flex min-h-[60vh] max-w-4xl flex-col gap-3 px-6 py-12"
    >
      <EmptyState
        size="lg"
        icon={<SettingsIcon />}
        title="Раздел в разработке"
        description="Управление политиками организации появится в версии 1.0.1"
      />
    </main>
  );
}
