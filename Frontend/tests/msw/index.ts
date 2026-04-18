// Barrel. Клиенты (тесты, Storybook) импортируют из '../../tests/msw'
// только то, что действительно нужно — пропадание browser.ts/server.ts
// при tree-shaking гарантировано разделением сред (node vs browser).

export * as fixtures from './fixtures';
export * from './handlers';
