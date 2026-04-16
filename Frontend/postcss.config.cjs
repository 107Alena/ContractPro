// CJS (.cjs) обязателен: package.json "type":"module" трактует .js как ESM,
// а PostCSS-loader Vite ждёт CommonJS `module.exports`. См. FE-TASK-017.
module.exports = {
  plugins: {
    tailwindcss: {},
    autoprefixer: {},
  },
};
