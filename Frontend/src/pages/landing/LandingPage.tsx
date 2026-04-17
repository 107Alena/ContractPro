import { useTranslation } from 'react-i18next';

/**
 * Временный placeholder для `/` (FE-TASK-030 App-shell).
 * Полная реализация — в последующем таске landing-страницы.
 */
export function LandingPage(): JSX.Element {
  const { t } = useTranslation();
  return (
    <main className="mx-auto flex min-h-[60vh] max-w-2xl flex-col items-center justify-center gap-4 px-6 py-16 text-center">
      <h1 className="text-3xl font-semibold text-fg">{t('app.name')}</h1>
      <p className="text-base text-fg-muted">{t('app.tagline')}</p>
    </main>
  );
}
