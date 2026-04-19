/**
 * Commitlint configuration for ContractPro Frontend.
 *
 * Enforces Conventional Commits specification with a restricted type-enum
 * aligned to the project's release/changelog tooling.
 *
 * @see https://www.conventionalcommits.org/
 * @see https://commitlint.js.org/
 */
module.exports = {
  extends: ['@commitlint/config-conventional'],
  rules: {
    'type-enum': [
      2,
      'always',
      [
        'feat',
        'fix',
        'refactor',
        'test',
        'docs',
        'chore',
        'build',
        'ci',
        'perf',
        'revert',
        'style',
      ],
    ],
  },
};
