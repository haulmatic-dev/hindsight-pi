import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';

/**
 * Maximum output to capture from a prd-lint subprocess.
 */
const LINT_MAX_OUTPUT = 2 * 1024 * 1024;

/**
 * Maximum characters to include in a PRD lint failure report.
 */
const LINT_FAILURE_SNIPPET = 2000;

/**
 * How long to wait for prd-lint to finish.
 */
const LINT_TIMEOUT_MS = 30_000;

/**
 * Defensive timeout for the PATH lookup so a misbehaving shell cannot block.
 */
const PATH_LOOKUP_TIMEOUT_MS = 10_000;

/**
 * Resolve the prd-lint binary path.
 *
 * Order of resolution:
 *   1. $HYDRA_TOOLS_DIR/prd-lint/prd-lint
 *   2. <cwd>/tools/prd-lint/prd-lint
 *   3. Any executable named `prd-lint` on PATH
 *
 * Note: there is deliberately no hardcoded host checkout path — an absolute
 * fallback breaks hermetic tests (they run in a temp cwd but would still find
 * the host's binary). $HYDRA_TOOLS_DIR is the sanctioned override.
 *
 * @returns {string|null} Absolute path to the binary, or null if not found.
 */
export function resolvePrdLintBinary() {
  const candidates = [
    process.env.HYDRA_TOOLS_DIR &&
      path.join(process.env.HYDRA_TOOLS_DIR, 'prd-lint', 'prd-lint'),
    path.join(process.cwd(), 'tools', 'prd-lint', 'prd-lint'),
  ].filter(Boolean);

  for (const candidate of candidates) {
    try {
      if (fs.existsSync(candidate) && fs.statSync(candidate).isFile()) {
        return path.resolve(candidate);
      }
    } catch {
      // Ignore and continue to the next candidate.
    }
  }

  // Fall back to PATH lookup via `command -v` with a defensive timeout.
  try {
    const pathCheck = spawnSync('/bin/sh', ['-c', 'command -v prd-lint'], {
      encoding: 'utf8',
      timeout: PATH_LOOKUP_TIMEOUT_MS,
    });
    if (pathCheck.status === 0 && pathCheck.stdout) {
      return pathCheck.stdout.trim() || null;
    }
  } catch {
    // PATH lookup failed or timed out — treat as not found.
  }

  return null;
}

/**
 * Execute prd-lint against a PRD.
 *
 * @param {string} binary - Absolute path to the prd-lint binary.
 * @param {string} prdPath - Absolute path to the PRD file.
 * @returns {import('node:child_process').SpawnSyncReturns<string>}
 */
function runLint(binary, prdPath) {
  return spawnSync(binary, [prdPath, '--json'], {
    encoding: 'utf8',
    maxBuffer: LINT_MAX_OUTPUT,
    timeout: LINT_TIMEOUT_MS,
  });
}

/**
 * Determine whether prd-lint crashed or could not be executed.
 *
 * Treat spawn errors, no exit status, and exit codes >= 2 as fail-open.
 *
 * @param {import('node:child_process').SpawnSyncReturns<string>} lintResult
 * @returns {boolean}
 */
function isLintCrash(lintResult) {
  return (
    Boolean(lintResult.error) ||
    lintResult.status === null ||
    lintResult.status >= 2
  );
}

/**
 * Build a short, human-readable diagnostic for a crashed lint run.
 *
 * @param {import('node:child_process').SpawnSyncReturns<string>} lintResult
 * @returns {string}
 */
function crashDiagnostic(lintResult) {
  if (lintResult.error) {
    return lintResult.error.message;
  }
  if (lintResult.signal) {
    return `killed by ${lintResult.signal}`;
  }
  return `exited ${lintResult.status}`;
}

/**
 * Capture the first part of the linter output for failure reporting.
 *
 * @param {import('node:child_process').SpawnSyncReturns<string>} lintResult
 * @returns {string}
 */
function lintOutputSnippet(lintResult) {
  return (lintResult.stdout || lintResult.stderr || '').slice(
    0,
    LINT_FAILURE_SNIPPET,
  );
}

/**
 * Possible outcomes of the preflight check.
 *
 * @typedef {Object} PreflightResult
 * @property {'passed'|'skipped'|'failed'} status
 * @property {string} [output]  - Linter output when status is 'failed'.
 * @property {string} [reason]  - Human-readable reason when status is 'skipped'.
 */

/**
 * Run the prd-lint preflight before task decomposition.
 *
 * The check is fail-open: a missing binary, `--no-lint`, or a crashed linter
 * all allow decomposition to continue. Only a clean exit 1 blocks.
 *
 * @param {Object} options
 * @param {string} options.prdPath - Absolute path to the PRD file.
 * @param {boolean} options.noLint - Whether to bypass the preflight.
 * @returns {PreflightResult}
 */
export function runPreflight(options) {
  if (options.noLint) {
    return { status: 'skipped', reason: '--no-lint' };
  }

  const binary = resolvePrdLintBinary();

  if (!binary) {
    return { status: 'skipped', reason: 'prd-lint not found' };
  }

  const lintResult = runLint(binary, options.prdPath);

  if (isLintCrash(lintResult)) {
    return {
      status: 'skipped',
      reason: `prd-lint ${crashDiagnostic(lintResult)}`,
      output: lintOutputSnippet(lintResult),
    };
  }

  if (lintResult.status === 0) {
    return { status: 'passed' };
  }

  return { status: 'failed', output: lintOutputSnippet(lintResult) };
}
