#!/usr/bin/env node
/**
 * prepare-husky.cjs
 *
 * Idempotently provisions git hooks for the Frontend subproject of a
 * monorepo whose `.git` directory lives at the repository root.
 *
 * Responsibilities:
 *   1. Ensures `Frontend/.husky/` exists.
 *   2. Writes `pre-commit` and `commit-msg` hook scripts with correct content
 *      and executable permissions.
 *   3. Points git's `core.hooksPath` at `Frontend/.husky` (global to the repo)
 *      so hooks fire regardless of where the user runs `git commit`.
 *
 * Invoked from the `prepare` npm lifecycle script after every `npm install`,
 * so hooks are always up-to-date for everyone on the team.
 *
 * This file is authored in CommonJS (.cjs) because the root package.json
 * declares `"type": "module"` and npm lifecycle scripts run this file
 * directly with node.
 */

'use strict';

const fs = require('node:fs');
const path = require('node:path');
const { execSync } = require('node:child_process');

const PRE_COMMIT_BODY = `#!/usr/bin/env sh
HUSKY_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$HUSKY_DIR/.." || exit 1
npx --no-install lint-staged
`;

const COMMIT_MSG_BODY = `#!/usr/bin/env sh
# Resolve $1 (git passes it relative to repo root CWD) to absolute path BEFORE cd.
MSG_FILE_ABS="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
HUSKY_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$HUSKY_DIR/.." || exit 1
npx --no-install commitlint --edit "$MSG_FILE_ABS"
`;

/**
 * Absolute path to the Frontend/ project root (one level up from scripts/).
 * @type {string}
 */
const frontendDir = path.resolve(__dirname, '..');

/**
 * Absolute path to the Frontend/.husky directory.
 * @type {string}
 */
const huskyDir = path.join(frontendDir, '.husky');

/**
 * Write a hook file with 0o755 permissions (rwxr-xr-x) idempotently.
 * @param {string} name Hook filename (e.g. "pre-commit").
 * @param {string} body Full script body including shebang.
 */
function writeHook(name, body) {
  const filePath = path.join(huskyDir, name);
  fs.writeFileSync(filePath, body, { encoding: 'utf8' });
  fs.chmodSync(filePath, 0o755);
  console.log(`[prepare-husky] wrote ${path.relative(frontendDir, filePath)}`);
}

function main() {
  // Skip when not inside a git working tree (e.g. `npm install` inside a
  // published tarball or a CI stage that operates on extracted sources).
  try {
    execSync('git rev-parse --is-inside-work-tree', {
      stdio: ['ignore', 'ignore', 'ignore'],
      cwd: frontendDir,
    });
  } catch {
    console.log('[prepare-husky] not a git working tree — skipping');
    return;
  }

  fs.mkdirSync(huskyDir, { recursive: true });

  writeHook('pre-commit', PRE_COMMIT_BODY);
  writeHook('commit-msg', COMMIT_MSG_BODY);

  // Point git at our hooks directory (relative to repo root) so hooks fire
  // regardless of the CWD from which `git commit` is invoked.
  const repoRoot = execSync('git rev-parse --show-toplevel', {
    cwd: frontendDir,
  })
    .toString()
    .trim();
  const hooksPathRelToRoot = path.relative(repoRoot, huskyDir);
  execSync(`git config core.hooksPath "${hooksPathRelToRoot}"`, {
    cwd: frontendDir,
    stdio: 'inherit',
  });
  console.log(`[prepare-husky] core.hooksPath -> ${hooksPathRelToRoot}`);
}

main();
