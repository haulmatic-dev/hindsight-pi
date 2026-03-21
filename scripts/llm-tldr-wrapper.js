#!/usr/bin/env node

/**
 * llm-tldr wrapper script with logging
 * Logs all llm-tldr commands and their responses
 *
 * Usage: Same as llm-tldr command
 * Example: llm-tldr search "authentication"
 */

const { spawn } = require('node:child_process');
const path = require('node:path');
const fs = require('node:fs');

// Find the real llm-tldr binary, excluding this wrapper script
// Use 'which' command with modified PATH (excluding hydra/bin) to avoid recursion
const { execSync } = require('node:child_process');
let TLDR_BINARY;
try {
  // Get original PATH without hydra/bin to find the real binary
  const originalPath = process.env.PATH.replace(/[^:]*hydra\/bin:?/g, '');
  TLDR_BINARY = execSync('which llm-tldr', {
    encoding: 'utf8',
    env: { ...process.env, PATH: originalPath },
  }).trim();
} catch (_err) {
  console.error('[tldr-wrapper] ERROR: Could not find real llm-tldr binary');
  process.exit(1);
}
const LOG_DIR = '/tmp/hydra-logs';
const LOG_FILE = path.join(LOG_DIR, 'tldr.log');
const MAX_LOG_SIZE = 10 * 1024 * 1024; // 10MB

// Ensure log directory exists
if (!fs.existsSync(LOG_DIR)) {
  fs.mkdirSync(LOG_DIR, { recursive: true });
}

// Rotate log if too large
function rotateLogIfNeeded() {
  try {
    if (fs.existsSync(LOG_FILE)) {
      const stats = fs.statSync(LOG_FILE);
      if (stats.size > MAX_LOG_SIZE) {
        const rotatedFile = `${LOG_FILE}.${Date.now()}`;
        fs.renameSync(LOG_FILE, rotatedFile);
        console.error(`[tldr-wrapper] Rotated log to ${rotatedFile}`);
      }
    }
  } catch (_err) {
    // Non-fatal, continue without rotation
  }
}

// Log entry format
function createLogEntry(type, data) {
  return `${JSON.stringify({
    timestamp: new Date().toISOString(),
    type,
    data,
    pid: process.pid,
    cwd: process.cwd(),
  })}\n`;
}

// Main execution
async function main() {
  rotateLogIfNeeded();

  const args = process.argv.slice(2);
  const startTime = Date.now();

  // Log the command
  const logEntry = createLogEntry('request', {
    command: 'llm-tldr',
    args,
    env: {
      HYDRA_BEAD_ID: process.env.HYDRA_BEAD_ID,
      HYDRA_PROJECT: process.env.HYDRA_PROJECT,
      HYDRA_AGENT_TYPE: process.env.HYDRA_AGENT_TYPE,
    },
  });

  try {
    fs.appendFileSync(LOG_FILE, logEntry);
  } catch (err) {
    console.error(`[tldr-wrapper] Failed to log request: ${err.message}`);
  }

  // Execute the real llm-tldr command
  const child = spawn(TLDR_BINARY, args, {
    stdio: ['inherit', 'pipe', 'pipe'],
    cwd: process.cwd(),
    env: process.env,
  });

  let stdout = '';
  let stderr = '';

  child.stdout.on('data', (data) => {
    stdout += data.toString();
    process.stdout.write(data);
  });

  child.stderr.on('data', (data) => {
    stderr += data.toString();
    process.stderr.write(data);
  });

  child.on('close', (code) => {
    const duration = Date.now() - startTime;

    // Log the response
    const responseEntry = createLogEntry('response', {
      command: 'llm-tldr',
      args,
      exitCode: code,
      duration,
      stdoutLength: stdout.length,
      stderrLength: stderr.length,
      stdout: stdout.substring(0, 10000), // Limit logged stdout
      stderr: stderr.substring(0, 1000), // Limit logged stderr
    });

    try {
      fs.appendFileSync(LOG_FILE, responseEntry);
    } catch (err) {
      console.error(`[tldr-wrapper] Failed to log response: ${err.message}`);
    }

    process.exit(code);
  });

  child.on('error', (err) => {
    const errorEntry = createLogEntry('error', {
      command: 'llm-tldr',
      args,
      error: err.message,
      duration: Date.now() - startTime,
    });

    try {
      fs.appendFileSync(LOG_FILE, errorEntry);
    } catch (logErr) {
      console.error(`[tldr-wrapper] Failed to log error: ${logErr.message}`);
    }

    console.error(`[tldr-wrapper] Failed to execute llm-tldr: ${err.message}`);
    process.exit(1);
  });
}

main();
