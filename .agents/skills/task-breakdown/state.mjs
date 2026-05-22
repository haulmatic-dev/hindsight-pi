/**
 * StateManager - JSON-based state persistence for task breakdown runner
 * 
 * Manages state across the 6-step process with atomic writes and resume capability
 */

import { promises as fs } from 'fs';
import path from 'path';

export class StateManager {
  constructor(sessionId) {
    this.sessionId = sessionId;
    this.baseDir = path.join(process.cwd(), '.pi', 'task-breakdown', sessionId);
    this.statePath = path.join(this.baseDir, 'state.json');
    this.logPath = path.join(this.baseDir, 'log.jsonl');
    this.errorsPath = path.join(this.baseDir, 'errors.json');
  }

  /**
   * Initialize state for a new task breakdown session
   * @param {string} prdPath - Path to the PRD file
   * @returns {Object} The initialized state object
   */
  async initialize(prdPath) {
    await fs.mkdir(this.baseDir, { recursive: true });
    
    const state = {
      sessionId: this.sessionId,
      startedAt: new Date().toISOString(),
      currentStep: 'validate-prd',
      prdPath,
      prdContent: null,
      parsedPRD: null,
      taskGraph: null,
      idMapping: {},
      validationIssues: [],
      errors: [],
      retryCount: {},
      tddState: {
        redPhase: false,
        greenPhase: false,
        refactorPhase: false,
        testsWritten: [],
        testsPassing: [],
        currentPhase: 'none'
      }
    };
    
    await this.save(state);
    await this.log('initialize', { prdPath });
    
    return state;
  }

  /**
   * Load current state from disk
   * @returns {Object} The current state object
   * @throws {Error} If state file doesn't exist or is corrupted
   */
  async load() {
    try {
      const content = await fs.readFile(this.statePath, 'utf8');
      return JSON.parse(content);
    } catch (error) {
      throw new Error(`Failed to load state: ${error.message}`);
    }
  }

  /**
   * Save state atomically (write to temp, then rename)
   * @param {Object} state - The state object to save
   */
  async save(state) {
    const tempPath = `${this.statePath}.tmp`;
    await fs.writeFile(tempPath, JSON.stringify(state, null, 2));
    await fs.rename(tempPath, this.statePath);
    
    // Also append to log
    await this.log('save', { 
      step: state.currentStep,
      timestamp: new Date().toISOString()
    });
  }

  /**
   * Update state with partial updates
   * @param {Object} updates - Partial state updates to merge
   * @returns {Object} The updated state object
   */
  async update(updates) {
    const state = await this.load();
    Object.assign(state, updates);
    await this.save(state);
    return state;
  }

  /**
   * Log an action to the log file
   * @param {string} action - The action name
   * @param {Object} details - Details about the action
   */
  async log(action, details) {
    const entry = {
      timestamp: new Date().toISOString(),
      action,
      details
    };
    await fs.appendFile(this.logPath, JSON.stringify(entry) + '\n');
  }

  /**
   * Log an error to errors.json and state.errors
   * @param {string} step - The step where the error occurred
   * @param {Error} error - The error object
   */
  async logError(step, error) {
    const state = await this.load();
    if (!state.errors) {
      state.errors = [];
    }
    state.errors.push({
      timestamp: new Date().toISOString(),
      step,
      message: error.message,
      stack: error.stack
    });
    
    // Save errors separately for easier debugging
    await fs.writeFile(
      this.errorsPath, 
      JSON.stringify(state.errors, null, 2)
    );
    
    await this.save(state);
  }

  /**
   * Increment retry count for a step
   * @param {string} step - The step name
   * @returns {number} The new retry count
   */
  async incrementRetry(step) {
    const state = await this.load();
    state.retryCount[step] = (state.retryCount[step] || 0) + 1;
    await this.save(state);
    return state.retryCount[step];
  }

  /**
   * Get retry count for a step
   * @param {string} step - The step name
   * @returns {number} The current retry count
   */
  async getRetryCount(step) {
    const state = await this.load();
    return state.retryCount[step] || 0;
  }

  /**
   * Check if we should retry a step
   * @param {string} step - The step name
   * @param {number} maxRetries - Maximum allowed retries (default: 3)
   * @returns {boolean} Whether we should retry
   */
  async shouldRetry(step, maxRetries = 3) {
    const count = await this.getRetryCount(step);
    return count < maxRetries;
  }

  /**
   * Get all created IDs for rollback
   * @returns {Array} Array of [localId, realId] pairs
   */
  async getCreatedIds() {
    const state = await this.load();
    return Object.entries(state.idMapping);
  }

  /**
   * Update TDD state
   * @param {Object} tddUpdates - Updates to merge into tddState
   * @returns {Object} The updated tddState
   */
  async updateTDDState(tddUpdates) {
    const state = await this.load();
    Object.assign(state.tddState, tddUpdates);
    await this.save(state);
    return state.tddState;
  }

  /**
   * Clean up state files (call after successful completion)
   * Marks state as completed but keeps files for 7 days
   */
  async cleanup() {
    // Keep for 7 days, then delete
    // For now, just mark as completed
    const state = await this.load();
    state.currentStep = 'completed';
    state.completedAt = new Date().toISOString();
    await this.save(state);
  }
}

export default StateManager;