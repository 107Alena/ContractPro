// ESLint 9 flat config для ContractPro frontend.
// Источник правил FSD — Frontend/architecture/high-architecture.md §2.1
// (app → processes → pages → widgets → features → entities → shared).
// Зависимости вверх запрещены; горизонтальные импорты между слайсами
// одного слоя запрещены (slice isolation через capture-group).
// Сегменты shared (api/auth/ui/lib/config/i18n/observability) считаются
// flat и не изолируются друг от друга (FSD v2: segments, not slices).
// NB: строка '${from.slice}' — шаблон-подстановка самого eslint-plugin-boundaries
// (одинарные кавычки, не JS template literal). Не менять.
import js from '@eslint/js';
import prettierConfig from 'eslint-config-prettier';
import boundariesPlugin from 'eslint-plugin-boundaries';
import importPlugin from 'eslint-plugin-import';
import jsxA11yPlugin from 'eslint-plugin-jsx-a11y';
import reactPlugin from 'eslint-plugin-react';
import reactHooksPlugin from 'eslint-plugin-react-hooks';
import simpleImportSort from 'eslint-plugin-simple-import-sort';
import globals from 'globals';
import tseslint from 'typescript-eslint';

const FSD_ELEMENTS = [
  { type: 'app', pattern: 'src/app/**/*', mode: 'file' },
  { type: 'processes', pattern: 'src/processes/*', capture: ['slice'], mode: 'folder' },
  { type: 'pages', pattern: 'src/pages/*', capture: ['slice'], mode: 'folder' },
  { type: 'widgets', pattern: 'src/widgets/*', capture: ['slice'], mode: 'folder' },
  { type: 'features', pattern: 'src/features/*', capture: ['slice'], mode: 'folder' },
  { type: 'entities', pattern: 'src/entities/*', capture: ['slice'], mode: 'folder' },
  { type: 'shared', pattern: 'src/shared/**/*', mode: 'file' },
];

const sliceSame = (type) => [type, { slice: '${from.slice}' }];

export default tseslint.config(
  {
    ignores: [
      'dist/**',
      'node_modules/**',
      'coverage/**',
      'storybook-static/**',
      'playwright-report/**',
      'test-results/**',
      // Vite public/ — статика, раздаётся as-is; не является частью ES-graph
      // приложения. mockServiceWorker.js (MSW) и config.js (runtime-env,
      // FE-TASK-009) лежат здесь.
      'public/**',
      'src/shared/api/openapi.d.ts',
      '.storybook/**/*.mdx',
    ],
  },

  js.configs.recommended,
  ...tseslint.configs.recommended,

  {
    files: ['src/**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
      parserOptions: {
        ecmaFeatures: { jsx: true },
      },
      globals: {
        ...globals.browser,
        ...globals.es2022,
      },
    },
    plugins: {
      react: reactPlugin,
      'react-hooks': reactHooksPlugin,
      'jsx-a11y': jsxA11yPlugin,
      import: importPlugin,
      boundaries: boundariesPlugin,
      'simple-import-sort': simpleImportSort,
    },
    settings: {
      react: { version: '18.3' },
      'import/resolver': {
        typescript: { project: './tsconfig.json' },
        node: true,
      },
      'boundaries/elements': FSD_ELEMENTS,
      'boundaries/include': ['src/**/*'],
      'boundaries/ignore': [
        '**/*.test.{ts,tsx}',
        '**/*.spec.{ts,tsx}',
        '**/*.stories.{ts,tsx}',
        'src/main.tsx',
        'src/vite-env.d.ts',
        // test-setup.ts — vitest setupFiles target; не является FSD-элементом.
        'src/test-setup.ts',
      ],
    },
    rules: {
      ...reactPlugin.configs.recommended.rules,
      ...reactPlugin.configs['jsx-runtime'].rules,
      ...reactHooksPlugin.configs.recommended.rules,
      ...jsxA11yPlugin.configs.recommended.rules,

      'react/prop-types': 'off',

      'simple-import-sort/imports': 'error',
      'simple-import-sort/exports': 'error',

      'boundaries/element-types': [
        'error',
        {
          default: 'disallow',
          rules: [
            {
              from: 'app',
              allow: ['app', 'processes', 'pages', 'widgets', 'features', 'entities', 'shared'],
            },
            {
              from: 'processes',
              allow: ['pages', 'widgets', 'features', 'entities', 'shared', sliceSame('processes')],
            },
            {
              from: 'pages',
              // `processes` допустим для pages по архитектурной договорённости:
              // processes/auth-flow экспортирует login/logout/setNavigator, которые
              // вызываются прямо со страниц (LoginPage/TopBar). FSD v1 формально
              // располагает processes выше pages, но для auth-flow задокументировано
              // обратное направление импорта (progress.md #1450). Ограничиваем
              // деформацию явным allow-list, а не ослаблением default.
              allow: ['processes', 'widgets', 'features', 'entities', 'shared', sliceSame('pages')],
            },
            {
              from: 'widgets',
              allow: ['features', 'entities', 'shared', sliceSame('widgets')],
            },
            {
              from: 'features',
              // `processes` допустим для features по той же архитектурной
              // договорённости, что и для pages: processes/auth-flow
              // экспортирует logout() — используется features/auth/logout для
              // делегирования (feature-хук useLogout). Без этого widgets
              // вынужден был бы импортировать processes напрямую. Ограничиваем
              // деформацию explicit allow-list.
              allow: ['processes', 'entities', 'shared', sliceSame('features')],
            },
            {
              from: 'entities',
              allow: ['shared', sliceSame('entities')],
            },
            {
              from: 'shared',
              allow: ['shared'],
            },
          ],
        },
      ],
      'boundaries/no-private': ['error', { allowUncles: false }],
      'boundaries/no-unknown': 'error',
      // FE-TASK-007/030: структура FSD завершена. App.tsx переехал в src/app/;
      // main.tsx и vite-env.d.ts остаются под src/ (Vite-конвенция bootstrap)
      // и явно занесены в boundaries/ignore.
      'boundaries/no-unknown-files': 'error',
    },
  },

  {
    files: ['src/app/**/*.{ts,tsx}'],
    rules: {
      'boundaries/no-private': 'off',
    },
  },

  {
    files: ['src/main.tsx'],
    rules: {
      'boundaries/element-types': 'off',
      'boundaries/no-unknown': 'off',
      'boundaries/no-unknown-files': 'off',
      'boundaries/no-private': 'off',
    },
  },

  {
    files: ['vite.config.ts', 'eslint.config.js', '**/*.config.{js,ts,mjs,cjs}'],
    languageOptions: {
      globals: { ...globals.node },
    },
    plugins: {
      'simple-import-sort': simpleImportSort,
    },
    rules: {
      'simple-import-sort/imports': 'error',
      'simple-import-sort/exports': 'error',
    },
  },

  {
    // Node-side maintenance scripts (husky bootstrap, codegen helpers и т.п.).
    // CommonJS-модули Node, не часть ES-графа приложения.
    files: ['scripts/**/*.cjs'],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'commonjs',
      globals: { ...globals.node },
    },
    rules: {
      '@typescript-eslint/no-require-imports': 'off',
    },
  },

  {
    files: ['.storybook/**/*.{ts,tsx}'],
    languageOptions: {
      globals: { ...globals.node },
    },
    rules: {
      'boundaries/element-types': 'off',
      'boundaries/no-unknown': 'off',
      'boundaries/no-unknown-files': 'off',
      'boundaries/no-private': 'off',
    },
  },

  {
    // MSW handlers/fixtures/server/worker — тестовая инфраструктура (FE-TASK-054,
    // §10.3). Не является FSD-элементом. Алиас @/* резолвится через tsconfig
    // (tests/ попали в include) и eslint-import-resolver-typescript.
    files: ['tests/**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
      globals: {
        ...globals.browser,
        ...globals.node,
        ...globals.es2022,
      },
    },
    plugins: {
      import: importPlugin,
      'simple-import-sort': simpleImportSort,
    },
    settings: {
      'import/resolver': {
        typescript: { project: './tsconfig.json' },
        node: true,
      },
    },
    rules: {
      'simple-import-sort/imports': 'error',
      'simple-import-sort/exports': 'error',
      'boundaries/element-types': 'off',
      'boundaries/no-unknown': 'off',
      'boundaries/no-unknown-files': 'off',
      'boundaries/no-private': 'off',
    },
  },

  prettierConfig,
);
