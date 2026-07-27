#!/usr/bin/env node

/**
 * CLI Wrapper for TaskBreakdownRunner
 *
 * Usage: node cli.mjs <prd-path> [--json] [--session-id <id>] [--no-lint]
 * Example: node cli.mjs docs/PRD/calculator.md
 * Example: node cli.mjs docs/PRD/calculator.md --json
 * Example: node cli.mjs docs/PRD/calculator.md --json --session-id my-session
 */

import path from 'node:path';
import { runPreflight } from './preflight.mjs';
import { TaskBreakdownRunner } from './runner.mjs';

/**
 * Parse command line arguments into an options object.
 *
 * @param {string[]} argv - Raw process.argv array.
 * @returns {{prdPath: string|null, json: boolean, sessionId: string|null, noLint: boolean}} Parsed options.
 */
function parseArgs(argv) {
  const args = argv.slice(2);
  const options = {
    prdPath: null,
    json: false,
    sessionId: null,
    noLint: false,
  };

  for (let i = 0; i < args.length; i++) {
    const arg = args[i];

    if (arg === '--json') {
      options.json = true;
    } else if (arg === '--no-lint') {
      options.noLint = true;
    } else if (arg === '--session-id') {
      options.sessionId = args[++i];
    } else if (!arg.startsWith('--') && !options.prdPath) {
      options.prdPath = arg;
    }
  }

  return options;
}

/**
 * Validate that the parsed CLI options contain a PRD path.
 *
 * @param {{prdPath: string|null}} options - Parsed CLI options.
 * @returns {{valid: boolean, message?: string}} Validation result.
 */
function validateArgs(options) {
  if (!options.prdPath) {
    return { valid: false, message: 'PRD file path is required' };
  }
  return { valid: true };
}

/**
 * Return the provided session ID or generate a new one based on the current timestamp.
 *
 * @param {string|null} providedId - Session ID supplied by the user, if any.
 * @returns {string} Session ID to use.
 */
function getSessionId(providedId) {
  if (providedId) {
    return providedId;
  }
  return `cli-${Date.now()}`;
}

/**
 * Emit a structured event in JSON mode.
 *
 * @param {Object} options - Parsed CLI options.
 * @param {string} type - Event type.
 * @param {Object} [extra] - Additional event fields.
 */
function emitEvent(options, type, extra = {}) {
  if (options.json) {
    console.log(JSON.stringify({ type, timestamp: Date.now(), ...extra }));
  }
}

/**
 * Report the outcome of the prd-lint preflight.
 *
 * @param {Object} options - Parsed CLI options.
 * @param {import('./preflight.mjs').PreflightResult} preflight
 */
function reportPreflightOutcome(options, preflight) {
  if (preflight.status === 'passed') {
    emitEvent(options, 'lint:passed');
    return;
  }

  if (preflight.status === 'skipped') {
    const extra = preflight.output ? { warning: preflight.reason } : {};
    emitEvent(options, 'lint:skipped', extra);

    if (!options.json) {
      console.log(`${preflight.reason} — skipping preflight`);
    }
  }
}

/**
 * Main CLI entry point: parse arguments, run preflight, and execute the runner.
 *
 * @returns {Promise<void>}
 */
async function main() {
  const options = parseArgs(process.argv);
  const validation = validateArgs(options);

  if (!validation.valid) {
    if (options.json) {
      emitEvent(options, 'error', {
        code: 'MISSING_ARGUMENT',
        error: validation.message,
      });
    } else {
      console.error('❌ Error:', validation.message);
      console.error('');
      console.error(
        'Usage: node cli.mjs <prd-path> [--json] [--session-id <id>] [--no-lint]',
      );
      console.error('Example: node cli.mjs docs/PRD/calculator.md');
      console.error('Example: node cli.mjs docs/PRD/calculator.md --json');
    }
    process.exit(1);
  }

  const absolutePath = path.resolve(options.prdPath);

  // Run prd-lint preflight before constructing the runner.
  const preflight = runPreflight({
    prdPath: absolutePath,
    noLint: options.noLint,
  });

  if (preflight.status === 'failed') {
    if (options.json) {
      emitEvent(options, 'error', {
        code: 'PRD_LINT_FAILED',
        error: preflight.output,
      });
    } else {
      console.error('❌ PRD lint failed');
      console.error(preflight.output);
    }
    process.exit(1);
  }

  // Report fail-open outcomes (and clean passes in JSON mode).
  if (!options.noLint) {
    reportPreflightOutcome(options, preflight);
  }

  const sessionId = getSessionId(options.sessionId);

  const runner = new TaskBreakdownRunner({
    sessionId,
    agent: {
      timeout: 120000,
      model: 'claude-opus',
    },
  });

  let isShuttingDown = false;
  let exitCode = 0;

  // Handle graceful shutdown
  async function handleShutdown(signal) {
    if (isShuttingDown) {
      return;
    }
    isShuttingDown = true;

    if (!options.json) {
      console.error(`\n\n⚠️  Received ${signal}. Shutting down gracefully...`);
    }

    try {
      await runner.flush();
      runner.destroy();
    } catch {
      // Ignore errors during cleanup
    }

    process.exit(exitCode);
  }

  process.on('SIGINT', () => handleShutdown('SIGINT'));
  process.on('SIGTERM', () => handleShutdown('SIGTERM'));

  if (options.json) {
    // JSON Lines Mode
    runner.on('event', (event) => {
      console.log(JSON.stringify(event));
    });
  } else {
    // Human-Readable Mode
    console.log('🚀 Task Breakdown Runner CLI');
    console.log('============================');
    console.log(`📄 PRD: ${absolutePath}`);
    console.log(`🔑 Session: ${sessionId}`);
    console.log('');

    // Track progress for human-readable output
    let tasksCreated = 0;
    const phases = new Set();

    runner.on('event', (event) => {
      switch (event.type) {
        case 'workflow:start':
          console.log('📋 Starting workflow...');
          break;

        case 'phase:start':
          console.log(`\n▶️  Phase: ${event.phase}`);
          break;

        case 'phase:complete': {
          phases.add(event.phase);
          console.log(`  ✅ Phase complete: ${event.phase}`);
          break;
        }

        case 'phase:skip':
          console.log(`  ⏭️  Phase skipped: ${event.phase} (${event.reason})`);
          break;

        case 'validation:start':
          console.log('  📖 Reading PRD...');
          break;

        case 'validation:success':
          console.log(
            `  ✓ Validated: ${event.requirementsCount || 0} requirements found`,
          );
          break;

        case 'agent:start':
          console.log(`  🤖 Agent started: ${event.agentType}`);
          break;

        case 'agent:complete':
          console.log(`  ✓ Agent complete: ${event.featureName || 'Analysis'}`);
          break;

        case 'task:gate:created':
          console.log(
            `  🚪 Created gate: ${event.gateType} (${event.gateId || event.approvalId || 'N/A'})`,
          );
          break;

        case 'task:batch:start':
          console.log(
            `\n  📦 Creating ${event.requirementsCount || event.taskCount || '?'} tasks...`,
          );
          break;

        case 'task:created':
          tasksCreated++;
          process.stdout.write(`\r  📝 Tasks created: ${tasksCreated}`);
          break;

        case 'task:subtask:created':
          tasksCreated++;
          process.stdout.write(`\r  📝 Tasks created: ${tasksCreated}`);
          break;

        case 'task:batch:complete':
          process.stdout.write('\n');
          console.log(
            `  ✓ Batch complete: ${event.totalTasks || tasksCreated} total tasks`,
          );
          break;

        case 'verification:complete':
          process.stdout.write('\n');
          console.log('  ✓ Verification complete');
          console.log(`    - Discovery tasks: ${event.discoveryTasks || 0}`);
          console.log(
            `    - Implementation tasks: ${event.implementationTasks || 0}`,
          );
          console.log(`    - TDD sub-tasks: ${event.tddSubtasks || 0}`);
          break;

        case 'error:occurred':
          console.error(
            `\n  ❌ Error in ${event.step || 'workflow'}: ${event.error}`,
          );
          break;

        case 'retry:attempt':
          console.log(
            `  🔄 Retry attempt ${event.attempt}/${event.maxRetries} for ${event.step}`,
          );
          break;

        case 'workflow:complete':
          console.log('\n✅ Task Breakdown Complete!');
          break;
      }
    });
  }

  try {
    const result = await runner.run(absolutePath);

    if (!options.json) {
      console.log('');
      console.log('Summary:');
      console.log(`  Session ID: ${result.sessionId}`);
      console.log(`  Feature: ${result.featureName}`);
      console.log(`  Requirements: ${result.stats?.requirements || 0}`);
      console.log(`  Discovery Tasks: ${result.stats?.discoveryTasks || 0}`);
      console.log(
        `  Implementation Tasks: ${result.stats?.implementationTasks || 0}`,
      );
      console.log(`  TDD Sub-Tasks: ${result.stats?.tddSubTasks || 0}`);
      console.log(`  Total Tasks: ${result.stats?.totalTasks || 0}`);
      console.log(`  Approval Gate: ${result.approvalGate}`);
      console.log(`  Execution Time: ${result.executionTime}ms`);
    }

    // Flush pending events before exit
    await runner.flush();
    runner.destroy();

    process.exit(0);
  } catch (error) {
    exitCode = 1;

    if (options.json) {
      emitEvent(options, 'error', {
        code: 'RUNNER_ERROR',
        error: error.message,
        stack: error.stack,
      });
    } else {
      console.error('\n❌ Task Breakdown Failed!');
      console.error('Error:', error.message);
      if (process.env.DEBUG) {
        console.error('Stack:', error.stack);
      }
    }

    // Flush pending events before exit
    try {
      await runner.flush();
    } catch {
      // Ignore flush errors during error handling
    }
    runner.destroy();

    process.exit(exitCode);
  }
}

// Run main
main().catch((error) => {
  console.error('Fatal error:', error);
  process.exit(1);
});
