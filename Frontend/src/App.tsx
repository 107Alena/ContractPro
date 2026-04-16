// FE-TASK-017: временная тестовая страница для проверки Tailwind + tokens.css.
// Будет заменена на композицию providers в FE-TASK-030 (App shell).
export function App() {
  return (
    <div className="p-8 font-sans">
      <h1 className="text-2xl text-fg">Hello ContractPro</h1>
      <div className="mt-4 space-y-2">
        <div className="rounded-md bg-brand-500 px-4 py-2 text-white shadow-md">
          bg-brand-500 · #F55E12
        </div>
        <div className="rounded-md bg-risk-high px-4 py-2 text-white">bg-risk-high</div>
        <div className="rounded-md border border-border bg-bg-muted px-4 py-2 text-fg-muted">
          bg-bg-muted · text-fg-muted · border-border
        </div>
      </div>
    </div>
  );
}
