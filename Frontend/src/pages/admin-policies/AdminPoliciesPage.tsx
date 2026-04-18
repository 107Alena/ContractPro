/**
 * Placeholder для /admin/policies (FE-TASK-031). Маршрут защищён <RequireRole roles=['ORG_ADMIN']>.
 * Финальная имплементация — FE-TASK-001 (EmptyState) → FE-TASK-002 (полные формы по DESIGN-TASK-002).
 */
export function AdminPoliciesPage(): JSX.Element {
  return (
    <main
      data-testid="page-admin-policies"
      className="mx-auto flex min-h-[60vh] max-w-4xl flex-col gap-3 px-6 py-12"
    >
      <h1 className="text-2xl font-semibold text-fg">Политики организации</h1>
      <p className="text-base text-fg-muted">
        Управление политиками появится в FE-TASK-001/002. Сейчас доступен только маршрут.
      </p>
    </main>
  );
}
