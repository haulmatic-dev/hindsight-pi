/**
 * BeadsExecutor - Synchronous beads operations
 *
 * Creates beads with dependencies set at creation time
 */

import { execSync } from 'child_process';

export class BeadsExecutor {
  constructor() {
    this.beadsBinary = 'br'; // or full path
    this.createdTasks = new Map(); // Track created tasks to prevent duplicates
    this.existingTasks = new Map(); // Pre-loaded existing tasks from database
    this.preloadExistingTasks();
  }

  /**
   * Pre-load all existing tasks from database to avoid repeated queries
   */
  preloadExistingTasks() {
    try {
      const cmd = `${this.beadsBinary} list --json`;
      const result = execSync(cmd, {
        encoding: 'utf8',
        timeout: 30000,
        stdio: ['pipe', 'pipe', 'pipe'],
        maxBuffer: 50 * 1024 * 1024, // 50MB buffer for large task lists
      });
      const tasks = JSON.parse(result);

      // Index tasks by title for fast lookup
      for (const task of tasks) {
        if (task.title && task.id) {
          this.existingTasks.set(task.title, task.id);
        }
      }

      console.log(
        `  📋 Pre-loaded ${this.existingTasks.size} existing tasks from database`,
      );
    } catch (error) {
      console.warn(`  ⚠️ Could not pre-load existing tasks: ${error.message}`);
    }
  }

  /**
   * Create approval gate with open status (blocks dependents until approved)
   * @param {Object} gate - Approval gate data
   * @returns {string} The created bead ID
   */
  createApprovalGate(gate) {
    // Check for existing approval gate with same title
    const existingId = this.findExistingTask(gate.title);
    if (existingId) {
      console.log(
        `  ⚠ Approval gate "${gate.title}" already exists (${existingId}), skipping creation`,
      );
      return existingId;
    }

    const flags = this.buildFlags(gate);

    // Default status (open) — gate blocks dependents via dependencies, not status
    const cmd = `${this.beadsBinary} create ${flags.join(' ')}`;

    try {
      const result = this.executeCommand(cmd);
      // Parse ID from output like "✓ Created e2e-test-project-xxx: Title"
      const match = result.match(/(?:✓ Created\s+)?([\w-]+):?/);
      const gateId = match ? match[1] : result.trim();

      // Cache the created gate in both caches
      this.createdTasks.set(gate.title, gateId);
      this.existingTasks.set(gate.title, gateId);

      return gateId;
    } catch (error) {
      throw new Error(`Failed to create approval gate: ${error.message}`);
    }
  }

  /**
   * Check if a task with the given title already exists
   * Uses pre-loaded cache for fast lookup (no database queries)
   * @param {string} title - Task title to check
   * @returns {string|null} Existing task ID or null if not found
   */
  findExistingTask(title) {
    // First check tasks created in this session
    if (this.createdTasks.has(title)) {
      return this.createdTasks.get(title);
    }

    // Then check pre-loaded existing tasks from database
    if (this.existingTasks.has(title)) {
      const existingId = this.existingTasks.get(title);
      // Promote to created tasks cache for consistency
      this.createdTasks.set(title, existingId);
      return existingId;
    }

    return null;
  }

  /**
   * Create task with dependencies
   * @param {Object} task - Task data
   * @param {Array} blockerIds - Array of blocker bead IDs
   * @returns {string} The created bead ID
   */
  createTask(task, blockerIds) {
    // Check for existing task with same title
    const existingId = this.findExistingTask(task.title);
    if (existingId) {
      console.log(
        `  ⚠ Task "${task.title}" already exists (${existingId}), skipping creation`,
      );
      return existingId;
    }

    const flags = this.buildFlags(task);

    // Add dependencies if any (using --deps flag)
    if (blockerIds && blockerIds.length > 0) {
      const depString = blockerIds.map((id) => `blocked-by:${id}`).join(',');
      flags.push(`--deps ${depString}`);
    }

    const cmd = `${this.beadsBinary} create ${flags.join(' ')} --status open`;

    try {
      const result = this.executeCommand(cmd);
      // Parse ID from output like "✓ Created e2e-test-project-xxx: Title"
      const match = result.match(/(?:✓ Created\s+)?([\w-]+):?/);
      const taskId = match ? match[1] : result.trim();

      // Cache the created task in both caches
      this.createdTasks.set(task.title, taskId);
      this.existingTasks.set(task.title, taskId);

      return taskId;
    } catch (error) {
      throw new Error(`Failed to create task ${task.title}: ${error.message}`);
    }
  }

  /**
   * Get list of ready tasks
   * @returns {Array} Array of ready task objects
   */
  getReadyTasks() {
    const cmd = `${this.beadsBinary} ready --json`;
    try {
      const result = this.executeCommand(cmd);
      return JSON.parse(result);
    } catch {
      return [];
    }
  }

  /**
   * Get dependencies (blockers) for a task
   * @param {string} taskId - The task bead ID
   * @returns {Array} Array of blocker IDs
   */
  getDependencies(taskId) {
    const cmd = `${this.beadsBinary} show ${taskId} --json`;
    try {
      const result = this.executeCommand(cmd);
      const tasks = JSON.parse(result);
      // br show returns an array with a single task
      const task = Array.isArray(tasks) ? tasks[0] : tasks;
      // Extract IDs from dependencies array (which contains objects with id field)
      if (task && task.dependencies && Array.isArray(task.dependencies)) {
        return task.dependencies.map((dep) => dep.id);
      }
      return [];
    } catch {
      return [];
    }
  }

  /**
   * Build command flags from entity data
   * @param {Object} entity - Entity with title, description, etc.
   * @returns {Array} Array of flag strings
   */
  buildFlags(entity) {
    const flags = [];

    // Title (quoted and escaped to handle special chars)
    const escapedTitle = this.escapeShellArg(entity.title);
    flags.push(escapedTitle);

    // Description
    if (entity.description) {
      const escapedDesc = this.escapeShellArg(entity.description);
      flags.push(`--description ${escapedDesc}`);
    }

    // Priority
    if (entity.priority !== undefined && entity.priority !== null) {
      flags.push(`--priority ${entity.priority}`);
    }

    // Type
    if (entity.type) {
      flags.push(`--type ${entity.type}`);
    }

    // Labels - sanitize and escape each label
    if (entity.labels && entity.labels.length > 0) {
      const sanitizedLabels = entity.labels.map((l) =>
        this.sanitizeLabel(this.escapeShellArg(l).replace(/^'|'$/g, '')),
      );
      flags.push(`--labels ${sanitizedLabels.join(',')}`);
    }

    // Note: metadata not supported by br create, stored in description instead

    return flags;
  }

  /**
   * Escape string for shell safety
   * Handles: quotes, backticks, $(), newlines, and other special chars
   * @param {string} str - String to escape
   * @returns {string} Escaped string wrapped in single quotes
   */
  escapeShellArg(str) {
    if (!str) return "''";
    // Use single quotes and escape any single quotes inside
    // This prevents shell interpretation of special characters
    return "'" + str.replace(/'/g, "'\"'\"'") + "'";
  }

  /**
   * Sanitize label for beads compliance
   * Labels can only contain: alphanumeric, hyphen, underscore, colon
   * @param {string} label - Label to sanitize
   * @returns {string} Sanitized label
   */
  sanitizeLabel(label) {
    if (!label) return 'untitled';
    return label
      .toLowerCase()
      .replace(/\s+/g, '-') // Replace spaces with hyphens
      .replace(/[^a-z0-9\-_:.]/g, '') // Keep only allowed chars
      .replace(/^-+|-+$/g, ''); // Trim leading/trailing hyphens
  }

  /**
   * Add dependency relationship between tasks
   * @param {string} taskId - The task to add dependency to
   * @param {string} dependsOnId - The task that blocks taskId
   * @param {string} type - Dependency type (e.g., 'parent-child', 'blocks')
   * @returns {boolean} Success status
   */
  addDependency(taskId, dependsOnId, type = 'blocked-by') {
    const cmd = `${this.beadsBinary} dep add ${taskId} ${dependsOnId} --type ${type}`;

    try {
      this.executeCommand(cmd);
      return true;
    } catch (error) {
      console.warn(
        `Failed to add dependency ${type} from ${taskId} to ${dependsOnId}: ${error.message}`,
      );
      return false;
    }
  }

  /**
   * Execute command synchronously
   * @param {string} cmd - Command to execute
   * @returns {string} Command output
   */
  executeCommand(cmd) {
    return execSync(cmd, {
      encoding: 'utf8',
      timeout: 30000,
      stdio: ['pipe', 'pipe', 'pipe'],
    });
  }
}

export default BeadsExecutor;
