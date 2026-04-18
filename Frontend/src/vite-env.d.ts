/// <reference types="vite/client" />

interface ImportMetaEnv {
  /**
   * Включает MSW-worker в `src/main.tsx`. Значение 'true' ожидается в e2e-mode
   * (`vite --mode e2e` подхватывает `.env.e2e`, FE-TASK-055) и опционально
   * в ручной dev-разработке без backend (`.env.development.local`).
   * В prod-bundle отсутствует: ветка под `import.meta.env.DEV` DCE'ится
   * при `vite build`.
   */
  readonly VITE_ENABLE_MSW?: 'true' | 'false';
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
