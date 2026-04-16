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
        'src/App.tsx',
        'src/vite-env.d.ts',
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
              allow: ['widgets', 'features', 'entities', 'shared', sliceSame('pages')],
            },
            {
              from: 'widgets',
              allow: ['features', 'entities', 'shared', sliceSame('widgets')],
            },
            {
              from: 'features',
              allow: ['entities', 'shared', sliceSame('features')],
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
      // FE-TASK-007: структура FSD-папок создана, неклассифицированных файлов под src/
      // больше быть не должно (main.tsx/App.tsx/vite-env.d.ts — в boundaries/ignore
      // до переезда в src/app/ в FE-TASK-030).
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
    files: ['src/main.tsx', 'src/App.tsx'],
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

  prettierConfig,
);
