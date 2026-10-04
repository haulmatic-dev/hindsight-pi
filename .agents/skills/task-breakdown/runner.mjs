/**
 * TaskBreakdownRunner - Main orchestrator for deterministic task breakdown
 *
 * PURE DETERMINISTIC EXECUTION - No AI agents involved
 * All analysis and task creation happens via deterministic code
 *
 * Event-driven architecture with event types:
 * - Workflow events: workflow:start, workflow:complete
 * - Phase events: phase:start, phase:complete, phase:skip
 * - Validation events: validation:start, validation:success
 * - State events: state:save
 * - Task events: task:gate:created, task:batch:start, task:created, task:subtask:created, task:batch:phase:complete, task:batch:complete
 * - Error events: error:occurred, retry:attempt
 * - Rollback events: rollback:start, rollback:progress, rollback:complete
 * - Verification events: verification:complete
 *
 * Manages the 6-step state machine:
 * 1. validate-prd
 * 2. analyze-prd (deterministic code analysis)
 * 3. create-approval
 * 4. create-tasks (includes TDD sub-task creation)
 * 5. create-lint-gate
 * 6. verify
 */

import { EventEmitter } from 'events';
import { promises as fs } from 'fs';
import path from 'path';
import { analyzePRDDeterministic } from './analyzer.mjs';
import { BeadsExecutor } from './executor.mjs';

/**
 * Wildcard config-task target list: emitted when a configuration_task
 * requirement declares no concrete files (missing, '*', or 'TBD').
 * Serves both the task metadata (`files`) and the instruction text.
 */
const DEFAULT_CONFIG_TARGET_FILES = [
  'package.json (root)',
  'tsconfig.json (root)',
  'tsconfig.base.json (if tsconfig.json extends it)',
  'turbo.json',
  'biome.json',
  'apps/api/package.json',
  'apps/web/package.json',
  'packages/math/package.json',
];
import { RollbackManager } from './rollback.mjs';
import { StateManager } from './state.mjs';

export class TaskBreakdownRunner extends EventEmitter {
  constructor(options = {}) {
    super();
    this.sessionId = options.sessionId || `td-${Date.now()}`;
    this.state = new StateManager(this.sessionId);
    // Pure deterministic execution - no AI agents
    this.executor = new BeadsExecutor();
    this.rollback = new RollbackManager();

    // Event batching configuration
    this.batchInterval = options.batchInterval || 100;
    this.maxBatchSize = options.maxBatchSize || 10;
    this.eventBuffer = [];
    this.batchTimer = null;

    // Step handlers - PURE DETERMINISTIC WORKFLOW
    // Code analyzes → Code creates ALL tasks (no AI agents)
    this.steps = {
      'validate-prd': this.validatePRD.bind(this),
      'analyze-prd': this.analyzePRD.bind(this),
      'create-epics': this.createEpicsFromAnalysis.bind(this),
      'create-approval': this.createApprovalGate.bind(this),
      'create-tasks': this.createTasksFromAnalysis.bind(this),
      verify: this.verifyAndSummarize.bind(this),
    };
  }

  /**
   * Emit an event with internal batching
   * @private
   */
  _emitEvent(type, data = {}) {
    const event = {
      type,
      timestamp: Date.now(),
      sessionId: this.sessionId,
      ...data,
    };

    this.eventBuffer.push(event);

    // Flush immediately if buffer is full
    if (this.eventBuffer.length >= this.maxBatchSize) {
      this._flushEvents();
      return;
    }

    // Schedule flush if not already scheduled
    if (!this.batchTimer) {
      this.batchTimer = setTimeout(() => {
        this._flushEvents();
      }, this.batchInterval);
    }
  }

  /**
   * Flush buffered events to listeners
   * @private
   */
  _flushEvents() {
    if (this.eventBuffer.length === 0) return;

    // Emit all buffered events
    this.eventBuffer.forEach((event) => {
      this.emit('event', event);
    });

    // Clear buffer and timer
    this.eventBuffer = [];
    if (this.batchTimer) {
      clearTimeout(this.batchTimer);
      this.batchTimer = null;
    }
  }

  /**
   * Force flush any pending events
   * Call this before process exit to ensure all events are emitted
   */
  async flush() {
    this._flushEvents();
    // Small delay to allow event listeners to process
    await new Promise((resolve) => setTimeout(resolve, 50));
  }

  /**
   * Clean up resources
   */
  destroy() {
    this._flushEvents();
    this.removeAllListeners();
  }

  /**
   * Main entry point - run the full task breakdown process
   * @param {string} prdPath - Path to PRD file
   * @returns {Promise<Object>} Summary of created tasks
   */
  async run(prdPath) {
    console.log(`[Runner] Starting task breakdown for: ${prdPath}`);
    console.log(`[Runner] Session ID: ${this.sessionId}`);

    // Emit workflow start
    this._emitEvent('workflow:start', {
      prdPath,
      sessionId: this.sessionId,
      timestamp: Date.now(),
    });

    // Initialize state
    await this.state.initialize(prdPath);

    try {
      // Run state machine
      while (true) {
        const currentState = await this.state.load();
        const step = currentState.currentStep;

        console.log(`\n[Runner] Executing step: ${step}`);

        if (step === 'completed') {
          console.log('[Runner] Task breakdown completed successfully!');
          const summary = this.generateSummary(currentState);

          // Emit workflow complete
          this._emitEvent('workflow:complete', {
            success: true,
            summary,
            timestamp: Date.now(),
          });

          // Ensure all events are flushed
          await this.flush();

          return summary;
        }

        if (step === 'failed') {
          const error = new Error(
            `Task breakdown failed: ${currentState.errors.slice(-1)[0]?.message}`,
          );
          this._emitEvent('error:occurred', {
            step,
            error: error.message,
            timestamp: Date.now(),
          });
          throw error;
        }

        // Execute current step
        const handler = this.steps[step];
        if (!handler) {
          const error = new Error(`Unknown step: ${step}`);
          this._emitEvent('error:occurred', {
            step,
            error: error.message,
            timestamp: Date.now(),
          });
          throw error;
        }

        await handler(currentState);

        // Small delay for logging visibility
        await new Promise((r) => setTimeout(r, 100));
      }
    } catch (error) {
      console.error(`[Runner] Error: ${error.message}`);

      // Emit error occurred
      this._emitEvent('error:occurred', {
        error: error.message,
        stack: error.stack,
        timestamp: Date.now(),
      });

      await this.handleError(error);

      // Ensure all events are flushed before rethrowing
      await this.flush();

      throw error;
    }
  }

  /**
   * Step 1: Validate PRD
   */
  async validatePRD(state) {
    const stepName = 'validate-prd';
    console.log('[Step 1] Validating PRD...');

    // Emit phase start
    this._emitEvent('phase:start', {
      step: stepName,
      phase: 'validation',
      timestamp: Date.now(),
    });

    // Emit validation start
    this._emitEvent('validation:start', {
      prdPath: state.prdPath,
      timestamp: Date.now(),
    });

    // Read PRD content
    const prdContent = await this.readPrdFile(state.prdPath);

    // Parse PRD
    const parsedPRD = this.parsePRD(prdContent);

    // Emit validation success
    this._emitEvent('validation:success', {
      prdPath: state.prdPath,
      requirementsCount: parsedPRD.requirements?.length || 0,
      timestamp: Date.now(),
    });

    console.log('  ✓ PRD is valid');

    // Emit phase complete
    this._emitEvent('phase:complete', {
      step: stepName,
      phase: 'validation',
      nextStep: 'analyze-prd',
      timestamp: Date.now(),
    });

    await this.state.update({
      currentStep: 'analyze-prd',
      prdContent,
      parsedPRD,
    });

    // Emit state save
    this._emitEvent('state:save', {
      step: stepName,
      timestamp: Date.now(),
    });
  }

  /**
   * Step 2: Analyze PRD
   * Deterministic code analysis - no AI agents
   */
  async analyzePRD(state) {
    const stepName = 'analyze-prd';
    console.log('[Step 2] Analyzing PRD...');

    // Emit phase start
    this._emitEvent('phase:start', {
      step: stepName,
      phase: 'analysis',
      timestamp: Date.now(),
    });

    // Use deterministic analyzer (pure code, no AI)
    const analysis = analyzePRDDeterministic(state.prdContent);

    // Validate analysis structure
    this.validateAnalysis(analysis);

    // Validate bead schema for each requirement
    console.log('  🔍 Validating bead schema...');
    analysis.requirements.forEach((req, index) => {
      this.validateBeadSchema(req, index);
    });
    console.log(`  ✓ Schema validation passed for ${analysis.requirements.length} requirements`);

    console.log(
      `  ✓ Analyzed PRD: ${analysis.featureName} with ${analysis.requirements?.length || 0} requirements`,
    );

    // Emit state save
    this._emitEvent('state:save', {
      step: stepName,
      timestamp: Date.now(),
    });

    // Emit phase complete
    this._emitEvent('phase:complete', {
      step: stepName,
      phase: 'analysis',
      nextStep: 'create-epics',
      timestamp: Date.now(),
    });

    await this.state.update({
      currentStep: 'create-epics',
      analysis: analysis,
    });
  }

  /**
   * Step 2b: Create Epics from Analysis
   * Creates phase-based epics for organizing requirements
   */
  async createEpicsFromAnalysis(state) {
    const stepName = 'create-epics';
    console.log('[Step 2b] Creating epics from analysis...');

    // Emit phase start
    this._emitEvent('phase:start', {
      step: stepName,
      phase: 'epic-creation',
      timestamp: Date.now(),
    });

    const { analysis, idMapping = {} } = state;
    const requirements = analysis.requirements || [];

    // Auto-detect phases based on requirement groupings
    // For simplicity, we'll create 4 phases based on requirement distribution
    const phases = this.detectPhases(requirements);
    const epicIds = {};

    console.log(`  Creating ${phases.length} phase epics...`);

    // Create master epic
    const masterEpic = {
      localId: 'epic-master',
      title: `EPIC: ${analysis.featureName}`,
      description: `Master epic for ${analysis.featureName} implementation.\n\nTotal Requirements: ${requirements.length}\nPhases: ${phases.length}`,
      level: 0,
      priority: 0,
      type: 'epic',
      labels: ['prd:epic', 'type:epic', 'prd:master'],
      metadata: {
        epic_type: 'master',
        feature_name: analysis.featureName,
        total_requirements: requirements.length,
        total_phases: phases.length,
        global_context: analysis.globalContext,
      },
    };

    const masterEpicId = this.executor.createTask(masterEpic, []);
    epicIds['master'] = masterEpicId;
    idMapping['epic-master'] = masterEpicId;

    this._emitEvent('task:created', {
      step: stepName,
      localId: 'epic-master',
      taskId: masterEpicId,
      taskType: 'epic',
      epicType: 'master',
      timestamp: Date.now(),
    });

    console.log(`  ✓ Created master epic: ${masterEpicId}`);

    // Create phase epics
    for (let i = 0; i < phases.length; i++) {
      const phase = phases[i];
      const localId = `epic-phase-${i}`;

      const phaseEpic = {
        localId,
        title: `PHASE ${i}: ${phase.name}`,
        description: `Phase ${i} epic covering ${phase.requirements.length} requirements.\n\n${phase.description || ''}`,
        level: 1,
        dependsOn: i === 0 ? [] : [`epic-phase-${i - 1}`], // Each phase depends on previous
        priority: 1,
        type: 'epic',
        labels: ['prd:epic', 'type:epic', 'prd:phase', `prd:phase-${i}`],
        metadata: {
          epic_type: 'phase',
          phase_index: i,
          phase_name: phase.name,
          requirements: phase.requirements.map((r) => r.id),
          parent_epic: 'epic-master',
        },
      };

      const phaseEpicId = this.executor.createTask(
        phaseEpic,
        i === 0 ? [masterEpicId] : [epicIds[`phase-${i - 1}`], masterEpicId],
      );

      epicIds[`phase-${i}`] = phaseEpicId;
      idMapping[localId] = phaseEpicId;

      // Phase epic already depends on master via blocker list in createTask

      this._emitEvent('task:created', {
        step: stepName,
        localId,
        taskId: phaseEpicId,
        taskType: 'epic',
        epicType: 'phase',
        phaseIndex: i,
        timestamp: Date.now(),
      });

      console.log(
        `  ✓ Created phase ${i} epic: ${phaseEpicId} (${phase.requirements.length} reqs)`,
      );
    }

    // Store phase mapping in state
    const reqToPhase = new Map();
    phases.forEach((phase, idx) => {
      phase.requirements.forEach((req) => {
        reqToPhase.set(req.id, idx);
      });
    });

    // Emit phase complete
    this._emitEvent('phase:complete', {
      step: stepName,
      phase: 'epic-creation',
      nextStep: 'create-approval',
      timestamp: Date.now(),
    });

    await this.state.update({
      currentStep: 'create-approval',
      epicIds,
      phases,
      reqToPhase: Object.fromEntries(reqToPhase),
      idMapping,
    });

    // Emit state save
    this._emitEvent('state:save', {
      step: stepName,
      timestamp: Date.now(),
    });

    console.log(`  ✓ Created ${phases.length} phase epics`);
  }

  /**
   * Auto-detect phases from requirements
   * Groups requirements into logical phases
   */
  detectPhases(requirements) {
    const phases = [];
    const reqCount = requirements.length;

    if (reqCount <= 10) {
      // Small PRD: single phase
      phases.push({
        name: 'Implementation',
        description: 'Complete implementation',
        requirements: [...requirements],
      });
    } else if (reqCount <= 20) {
      // Medium PRD: 2 phases
      const mid = Math.ceil(reqCount / 2);
      phases.push({
        name: 'Foundation',
        description: 'Core infrastructure and basic features',
        requirements: requirements.slice(0, mid),
      });
      phases.push({
        name: 'Features',
        description: 'Advanced features and enhancements',
        requirements: requirements.slice(mid),
      });
    } else if (reqCount <= 30) {
      // Large PRD: 3 phases
      const size = Math.ceil(reqCount / 3);
      phases.push({
        name: 'Foundation',
        description: 'Core infrastructure',
        requirements: requirements.slice(0, size),
      });
      phases.push({
        name: 'Core Features',
        description: 'Primary functionality',
        requirements: requirements.slice(size, size * 2),
      });
      phases.push({
        name: 'Advanced',
        description: 'Advanced features and polish',
        requirements: requirements.slice(size * 2),
      });
    } else {
      // Very large PRD: 4 phases
      const size = Math.ceil(reqCount / 4);
      phases.push({
        name: 'Foundation & Architecture',
        description: 'Architecture, models, and core infrastructure',
        requirements: requirements.slice(0, size),
      });
      phases.push({
        name: 'Core Features',
        description: 'Primary functionality and integrations',
        requirements: requirements.slice(size, size * 2),
      });
      phases.push({
        name: 'Advanced Features',
        description: 'Advanced capabilities and services',
        requirements: requirements.slice(size * 2, size * 3),
      });
      phases.push({
        name: 'Quality & Deployment',
        description: 'Quality gates, testing, and deployment',
        requirements: requirements.slice(size * 3),
      });
    }

    return phases;
  }

  /**
   * Validate agent analysis output
   */
  validateAnalysis(analysis) {
    if (!analysis || typeof analysis !== 'object') {
      throw new Error('Analysis must be an object');
    }

    if (!analysis.featureName || typeof analysis.featureName !== 'string') {
      throw new Error('Analysis must have featureName string');
    }

    if (!Array.isArray(analysis.requirements)) {
      throw new Error('Analysis must have requirements array');
    }

    for (const req of analysis.requirements) {
      if (!req.id || typeof req.id !== 'string') {
        throw new Error('Each requirement must have an id');
      }
      if (!req.type || typeof req.type !== 'string') {
        throw new Error('Each requirement must have a type');
      }
      if (!req.title || typeof req.title !== 'string') {
        throw new Error('Each requirement must have a title');
      }
      if (!Array.isArray(req.acceptanceCriteria)) {
        req.acceptanceCriteria = []; // Default to empty array
      }
    }

    return true;
  }

  /**
   * Validate bead schema for required fields
   * Ensures all beads have consistent structure for Hydra execution
   */
  validateBeadSchema(req, index) {
    const requiredFields = [
      { field: 'id', type: 'string' },
      { field: 'originalId', type: 'string' },
      { field: 'title', type: 'string' },
      { field: 'type', type: 'string' },
      { field: 'taskPattern', type: 'string' },
      { field: 'needsWorktree', type: 'boolean' },
    ];

    const errors = [];

    for (const { field, type } of requiredFields) {
      if (req[field] === undefined || req[field] === null) {
        errors.push(`Missing required field: ${field}`);
      } else if (typeof req[field] !== type) {
        errors.push(`Invalid type for ${field}: expected ${type}, got ${typeof req[field]}`);
      }
    }

    // Validate dependencies
    if (!Array.isArray(req.dependsOn)) {
      errors.push(`dependsOn must be an array`);
    }

    // Validate taskPattern is one of allowed values
    const validPatterns = ['tdd_chain', 'direct_task', 'verification_gate', 'aggregation_task', 'configuration_task'];
    if (!validPatterns.includes(req.taskPattern)) {
      errors.push(`Invalid taskPattern: ${req.taskPattern}. Must be one of: ${validPatterns.join(', ')}`);
    }

    // Validate US classification
    if (req.type === 'User Story' && req.newLogicDescription) {
      console.log(`    📝 US ${req.originalId} classified as heavy (new logic: ${req.newLogicDescription})`);
    }

    if (errors.length > 0) {
      throw new Error(`Schema validation failed for requirement ${index + 1} (${req.originalId || 'unknown'}):\n  - ${errors.join('\n  - ')}`);
    }

    return true;
  }

  /**
   * Detect if project qualifies for discovery skip (greenfield optimization)
   * Requires 3 out of 4 conditions to be met for practical reliability
   */
  async shouldSkipDiscovery(state) {
    const { prdContent, parsedPRD } = state;

    // Check 1: PRD has comprehensive research (section 9+)
    const hasResearch = this.hasComprehensiveResearch(prdContent);

    // Check 2: Greenfield project (no existing src/ files in project root)
    const isGreenfield = await this.isGreenfieldProject();

    // Check 3: Self-contained feature (no external APIs/databases)
    const isSelfContained = this.isSelfContainedFeature(parsedPRD);

    // Check 4: Clear implementation approach
    const hasClearApproach = this.hasClearImplementationApproach(prdContent);

    // Score-based detection (need 3+ points)
    let score = 0;
    if (hasResearch) score++;
    if (isGreenfield) score++;
    if (isSelfContained) score++;
    if (hasClearApproach) score++;

    const shouldSkip = score >= 3;

    if (shouldSkip) {
      console.log('  🚀 Greenfield optimization: Skipping discovery phase');
      console.log(`     Score: ${score}/4 conditions met`);
    } else {
      console.log(`  i️  Discovery phase enabled (${score}/4 conditions met)`);
    }

    return shouldSkip;
  }

  hasComprehensiveResearch(prdContent) {
    // Check for Section 9+ or Research section
    const hasSection9 =
      /#{1,3}\s*(9|Research|Context|Best Practices|Domain|Technical Context)/i.test(
        prdContent,
      );
    // Check for detailed implementation guidance (substantial PRD)
    const hasImplementationGuidance = prdContent.length > 3000;
    return hasSection9 || hasImplementationGuidance;
  }

  async isGreenfieldProject() {
    try {
      const cwd = process.cwd();
      const dirsToCheck = ['src', 'lib', 'app', 'source', 'dist', 'build'];

      // Check for common source directories
      for (const dir of dirsToCheck) {
        const dirPath = path.join(cwd, dir);
        try {
          const entries = await fs.readdir(dirPath);
          if (entries.length > 0) {
            return false; // Found existing code
          }
        } catch {
          // Directory doesn't exist, continue checking
        }
      }

      // Check for source files in root
      const rootEntries = await fs.readdir(cwd);
      const sourceFilePattern = /\.(ts|js|tsx|jsx|py|go|rs|java)$/i;
      const hasSourceFiles = rootEntries.some((entry) =>
        sourceFilePattern.test(entry),
      );

      return !hasSourceFiles;
    } catch {
      return true; // Assume greenfield if we can't check
    }
  }

  isSelfContainedFeature(parsedPRD) {
    // Check requirements for external dependencies
    const requirements = parsedPRD?.requirements || [];

    for (const req of requirements) {
      const desc = (req.description || '').toLowerCase();
      const title = (req.title || '').toLowerCase();

      // Check for complex integration indicators
      const hasExternalAPI =
        /\b(api|endpoint|integration|service|database|db|third.party|external)\b/.test(
          desc + ' ' + title,
        );
      const hasComplexDeps =
        /\b(auth|payment|oauth|webhook|queue|cache|microservice|grpc|graphql|rest)\b/.test(
          desc + ' ' + title,
        );

      if (hasExternalAPI || hasComplexDeps) {
        return false;
      }
    }

    return true;
  }

  hasClearImplementationApproach(prdContent) {
    // Check for technology stack specification
    const hasTechStack =
      /\b(typescript|javascript|python|react|vue|angular|node|bun|deno|svelte|solid)\b/i.test(
        prdContent,
      );
    // Check for architecture patterns
    const hasPatterns =
      /\b(pattern|architecture|design|structure|approach|pattern|framework)\b/i.test(
        prdContent,
      );
    // Check for file specifications
    const hasFileSpecs =
      /#{1,4}.*files?:/i.test(prdContent) ||
      /`[^`]+\.(ts|js|tsx|jsx|py)`/.test(prdContent);

    return hasTechStack && (hasPatterns || hasFileSpecs);
  }

  /**
   * Step 3: Create Approval Gate
   */
  async createApprovalGate(state) {
    const stepName = 'create-approval';
    console.log('[Step 3] Creating approval gate...');

    // Emit phase start
    this._emitEvent('phase:start', {
      step: stepName,
      phase: 'approval-gate',
      timestamp: Date.now(),
    });

    const analysis = state.analysis;
    const approvalGate = {
      title: `APPROVE: ${analysis.featureName} Implementation Review`,
      description: `Review and approve implementation plan for ${analysis.featureName}.\n\nTotal Requirements: ${analysis.requirements?.length || 0}\nEstimated Hours: ${analysis.requirements?.reduce((sum, r) => sum + (r.estimatedHours || 0), 0) || 0}`,
      priority: 1,
      type: 'approval',
      labels: ['approval-gate', 'review'],
    };

    const approvalId = this.executor.createApprovalGate(approvalGate);

    // Emit task gate created
    this._emitEvent('task:gate:created', {
      step: stepName,
      gateType: 'approval',
      gateId: approvalId,
      featureName: analysis.featureName,
      requirementsCount: analysis.requirements?.length || 0,
      timestamp: Date.now(),
    });

    console.log(`  ✓ Created approval gate: ${approvalId}`);

    // Emit phase complete
    this._emitEvent('phase:complete', {
      step: stepName,
      phase: 'approval-gate',
      nextStep: 'create-tasks',
      timestamp: Date.now(),
    });

    await this.state.update({
      currentStep: 'create-tasks',
      idMapping: {
        ...state.idMapping,
        approvalGate: approvalId,
      },
    });

    // Emit state save
    this._emitEvent('state:save', {
      step: stepName,
      timestamp: Date.now(),
    });
  }

  /**
   * Step 4: Create Tasks from Analysis
   * DETERMINISTIC: Creates ALL tasks from agent analysis - no agent involvement
   */
  async createTasksFromAnalysis(state) {
    const stepName = 'create-tasks';
    console.log('[Step 4] Creating tasks from analysis...');

    // Emit phase start
    this._emitEvent('phase:start', {
      step: stepName,
      phase: 'task-creation',
      timestamp: Date.now(),
    });

    const { analysis, idMapping, reqToPhase, epicIds } = state;
    let requirements = analysis.requirements || [];
    const approvalId = idMapping.approvalGate;

    // GREENFIELD GUARD: If no package.json exists, inject a monorepo config task
    // so that TDD beads have vitest/tsconfig scaffolding to run tests.
    const hasPackageJson = await fs.access('package.json').then(() => true).catch(() => false);
    if (!hasPackageJson) {
      console.log('  🏗️  No package.json found — injecting monorepo configuration task');
      const configReq = {
        id: 'NFR-003',
        title: 'Monorepo Configuration',
        description: 'Set up TypeScript monorepo scaffolding: package.json, tsconfig.json, vitest, turbo, biome. This is a hard prerequisite for all TDD beads.',
        priority: 'P0',
        type: 'nfr',
        taskPattern: 'configuration_task',
        estimatedHours: 0.5,
        files: ['package.json', 'tsconfig.json', 'tsconfig.base.json', 'turbo.json', 'biome.json', 'packages/math/package.json', 'apps/api/package.json', 'apps/web/package.json'],
        acceptanceCriteria: [
          'package.json has workspaces array: ["apps/*", "packages/*"]',
          'tsconfig.json extends shared TypeScript config',
          'Root package.json has test, lint, build scripts',
          'npm install exits 0',
          'npm test exits 0',
        ],
      };
      requirements = [configReq, ...requirements];
      analysis.requirements = requirements;
    }

    // Sort requirements by priority first (P0 → P1 → P2), then by taskPattern,
    // then by estimated hours ascending (simple tasks first → more throughput)
    requirements = [...requirements].sort((a, b) => {
      const priorityOrder = { 'P0': 0, 'P1': 1, 'P2': 2 };
      const priorityDiff = (priorityOrder[a.priority] ?? 1) - (priorityOrder[b.priority] ?? 1);
      if (priorityDiff !== 0) return priorityDiff;

      const order = {
        'tdd_chain': 0,
        'direct_task': 1,
        'configuration_task': 2,
        'aggregation_task': 3,
        'verification_gate': 4,
      };
      const patternDiff = (order[a.taskPattern] || 0) - (order[b.taskPattern] || 0);
      if (patternDiff !== 0) return patternDiff;

      // Simple tasks first → more beads complete in limited time
      return (a.estimatedHours || 1) - (b.estimatedHours || 1);
    });

    // Track tasks per requirement for dependency resolution
    const reqTasks = new Map(); // reqId -> { discoveryId, implementationId }

    console.log(`  Creating tasks for ${requirements.length} requirements`);

    // Emit task batch start
    this._emitEvent('task:batch:start', {
      step: stepName,
      batchType: 'all-tasks',
      requirementsCount: requirements.length,
      timestamp: Date.now(),
    });

    // Check if we should skip discovery phase (greenfield optimization)
    const skipDiscovery = await this.shouldSkipDiscovery(state);

    // Phase 1: Create Discovery tasks (Level 1) - CONDITIONAL
    if (!skipDiscovery) {
      console.log('  Phase 1: Discovery tasks');

      // Emit batch phase start for discovery
      this._emitEvent('task:batch:phase:start', {
        step: stepName,
        phase: 'discovery',
        taskCount: requirements.length,
        timestamp: Date.now(),
      });

      for (const req of requirements) {
        const localId = `discovery-${req.id.toLowerCase().replace(/[^a-z0-9]/g, '-')}`;

        const discoveryTask = {
          localId,
          title: `Discovery: ${req.title}`,
          description: this.formatTaskDescription({
            req,
            phase: 'discovery',
            instructions: `Research and understand requirements for ${req.title}.\n\nReview the acceptance criteria below and identify any ambiguities or missing information.`,
          }),
          level: 1,
          dependsOn: ['approvalGate'],
          priority: 2,
          type: 'task',
          labels: ['discovery', req.id.toLowerCase()],
          metadata: {
            prd_requirement_id: req.id,
            estimated_hours: Math.min(req.estimatedHours * 0.2, 1),
            phase_index: reqToPhase?.[req.id] ?? 0,
            parent_epic: `epic-phase-${reqToPhase?.[req.id] ?? 0}`,
            data_models: req.dataModels || [],
          },
        };

        const discoveryId = this.executor.createTask(discoveryTask, [
          approvalId,
        ]);

        // Link to phase epic via parent-child relationship
        const phaseEpicId = epicIds?.[`phase-${reqToPhase?.[req.id] ?? 0}`];
        if (phaseEpicId) {
          this.executor.addDependency(phaseEpicId, discoveryId, 'parent-child');
        }

        // Emit task created
        this._emitEvent('task:created', {
          step: stepName,
          localId,
          taskId: discoveryId,
          taskType: 'discovery',
          requirementId: req.id,
          timestamp: Date.now(),
        });

        console.log(`    ✓ ${localId} → ${discoveryId}`);

        idMapping[localId] = discoveryId;
        reqTasks.set(req.id, { discoveryId, discoveryLocalId: localId });

        await this.state.update({ idMapping });
      }

      // Emit batch phase complete for discovery
      this._emitEvent('task:batch:phase:complete', {
        step: stepName,
        phase: 'discovery',
        tasksCreated: requirements.length,
        timestamp: Date.now(),
      });
    } else {
      console.log('  Phase 1: SKIPPED (greenfield optimization active)');

      // Emit phase skip
      this._emitEvent('phase:skip', {
        step: stepName,
        phase: 'discovery',
        reason: 'greenfield-optimization',
        timestamp: Date.now(),
      });
    }

    // Phase 2: Create Implementation tasks (Level 2)
    console.log('  Phase 2: Implementation tasks');

    // Emit batch phase start for implementation
    this._emitEvent('task:batch:phase:start', {
      step: stepName,
      phase: 'implementation',
      taskCount: requirements.length,
      timestamp: Date.now(),
    });

    for (const req of requirements) {
      const localId = `implement-${req.id.toLowerCase().replace(/[^a-z0-9]/g, '-')}`;

      // Get or initialize task info for this requirement
      let taskInfo = reqTasks.get(req.id);
      if (!taskInfo) {
        taskInfo = {};
        reqTasks.set(req.id, taskInfo);
      }

      // Build dependency list based on agent's dependency mapping
      let dependsOn;
      if (skipDiscovery) {
        // Greenfield mode: Only depend on approval gate
        dependsOn = ['approvalGate'];
      } else {
        // Normal mode: Depend on discovery tasks
        const discoveryLocalId = taskInfo.discoveryLocalId || 'approvalGate';
        dependsOn = [discoveryLocalId, 'approvalGate'];

        // Add cross-requirement dependencies from discovery tasks
        const crossDeps = analysis.dependencies?.[req.id] || [];
        for (const depReqId of crossDeps) {
          const depTaskInfo = reqTasks.get(depReqId);
          if (depTaskInfo?.discoveryLocalId) {
            dependsOn.push(depTaskInfo.discoveryLocalId);
          }
        }
      }

      // Build virtual task template for TDD subtask generation
      // (No parent tracker bead is created — TDD phases carry their own context)
      const baseTask = {
        localId,
        title: `${req.originalId || req.id}: ${req.title}`,
        level: 2,
        priority: ({ 'P0': 0, 'P1': 1, 'P2': 2, 'P3': 3 }[req.priority] ?? 1),
        labels: [
          req.type.toLowerCase(),
          req.id.toLowerCase(),
        ],
        metadata: {
          prd_requirement_id: req.id,
          estimated_hours: req.estimatedHours,
          files: req.files || [],
          functions: req.functions || [],
          data_models: req.dataModels || [],
          integration_points: req.integrationPoints || [],
          test_command: req.testCommand || '',
          build_command: req.buildCommand || '',
          phase_index: reqToPhase?.[req.id] ?? 0,
          parent_epic: `epic-phase-${reqToPhase?.[req.id] ?? 0}`,
          parent_criteria: req.acceptanceCriteria || [],
        },
      };

      taskInfo.baseTask = baseTask;
      taskInfo.baseLocalId = localId;

      // Phase 3: Create task pattern based on requirement type
      console.log(`      Creating ${req.taskPattern || 'TDD_CHAIN'} pattern for ${localId}`);

      // Build implicit priority dependencies: P1/P2 RED beads depend on all P0 APPLY beads
      // This ensures config files exist before features start writing code.
      // SYNTAX can run in parallel (or after) — we don't need lint-perfect config to begin coding.
      const p0ApplyIds = [];
      if ((req.priority === 'P1' || req.priority === 'P2') && requirements.some(r => r.priority === 'P0')) {
        for (const p0Req of requirements) {
          if (p0Req.priority === 'P0') {
            const p0ApplyLocalId = `implement-${p0Req.id.toLowerCase().replace(/[^a-z0-9]/g, '-')}-apply`;
            if (idMapping[p0ApplyLocalId]) {
              p0ApplyIds.push(p0ApplyLocalId);
            }
          }
        }
      }

      // Type-aware task creation
      if (req.taskPattern === 'verification_gate') {
        // Verification NFRs: single gate bead that runs after all implementation
        // Use req.dependsOn (explicit Verifies list) for blockers
        const verifiedReqIds = req.dependsOn || [];
        const gateDeps = ['approvalGate', ...verifiedReqIds];

        const gateTask = {
          localId,
          title: `${req.originalId || req.id}: ${req.title}`,
          description: this.formatTrackerDescription({
            req,
            instructions: `## Verification Gate

Run verification checks for ${req.originalId || req.id}.

This gate runs after all implementation tasks complete.`,
          }),
          level: 2,
          dependsOn: [...new Set(gateDeps)],
          priority: ({ 'P0': 0, 'P1': 1, 'P2': 2, 'P3': 3 }[req.priority] ?? 2),
          type: 'task',
          labels: [
            'nfr',
            'verification',
            req.id.toLowerCase(),
          ],
          metadata: {
            prd_requirement_id: req.id,
            verifies: req.explicitVerifies || [],
          },
        };

        const gateBlockers = gateDeps.map((dep) => {
          // Map requirement IDs to their SYNTAX task IDs
          const depReq = requirements.find(r => r.originalId === dep);
          if (depReq) {
            const depLocalId = `implement-${depReq.id.toLowerCase().replace(/[^a-z0-9]/g, '-')}-syntax`;
            return idMapping[depLocalId];
          }
          return idMapping[dep];
        }).filter(Boolean);

        const gateId = this.executor.createTask(gateTask, gateBlockers);
        console.log(`    ✓ ${localId} → ${gateId} (verification gate, blocked by ${gateBlockers.length} tasks)`);
        idMapping[localId] = gateId;
        taskInfo.gateId = gateId;
      } else if (req.taskPattern === 'aggregation_task') {
        // Aggregation tasks: RED depends on aggregated FR SYNTAX tasks + P0 APPLY tasks
        const aggregatedSyntaxIds = req.dependsOn.map(depId => {
          const depReq = requirements.find(r => r.originalId === depId);
          if (depReq) {
            return `implement-${depReq.id.toLowerCase().replace(/[^a-z0-9]/g, '-')}-syntax`;
          }
          return null;
        }).filter(Boolean);

        await this.createAggregationTask(req, baseTask, null, state, [...aggregatedSyntaxIds, ...p0ApplyIds]);
      } else if (req.taskPattern === 'configuration_task') {
        // Configuration tasks: RED depends on approval gate
        await this.createConfigurationTask(req, baseTask, null, state, ['approvalGate']);
      } else if (req.taskPattern === 'direct_task') {
        // Direct tasks: single bead, no RED→GREEN→REFACTOR chain; depends on
        // explicit Depends On (mapped to SYNTAX ids) + P0 APPLY tasks.
        let directDeps = ['approvalGate'];
        if (req.explicitDependsOn && req.explicitDependsOn.length > 0) {
          directDeps = req.explicitDependsOn.map(depId => {
            const depReq = requirements.find(r => r.originalId === depId);
            if (depReq) {
              return `implement-${depReq.id.toLowerCase().replace(/[^a-z0-9]/g, '-')}-syntax`;
            }
            return null;
          }).filter(Boolean);
        }
        directDeps = [...new Set([...directDeps, ...p0ApplyIds])];
        await this.createConfigurationTask(req, baseTask, null, state, directDeps);
      } else {
        // Default: TDD_CHAIN for FRs
        // RED depends on approval gate or explicit Depends On + P0 APPLY tasks
        let redDeps = ['approvalGate'];
        if (req.explicitDependsOn && req.explicitDependsOn.length > 0) {
          redDeps = req.explicitDependsOn.map(depId => {
            const depReq = requirements.find(r => r.originalId === depId);
            if (depReq) {
              return `implement-${depReq.id.toLowerCase().replace(/[^a-z0-9]/g, '-')}-syntax`;
            }
            return null;
          }).filter(Boolean);
        }
        // Add implicit P0 dependencies (APPLY, not SYNTAX)
        redDeps = [...new Set([...redDeps, ...p0ApplyIds])];

        const tddTasks = this.generateTDDSubTasks(baseTask, redDeps);
        let lastTddId = null;

        for (const tddTask of tddTasks) {
          const tddBlockers = tddTask.dependsOn.map((depLocalId) => {
            const realId = idMapping[depLocalId];
            if (!realId)
              throw new Error(`TDD dependency ${depLocalId} not found`);
            return realId;
          });

          const tddId = this.executor.createTask(tddTask, tddBlockers);
          lastTddId = tddId;

          this._emitEvent('task:subtask:created', {
            step: stepName,
            parentLocalId: localId,
            localId: tddTask.localId,
            taskId: tddId,
            subtaskType: tddTask.localId.split('-').pop(),
            timestamp: Date.now(),
          });

          console.log(`        ✓ ${tddTask.localId} → ${tddId}`);
          idMapping[tddTask.localId] = tddId;
        }
      }

      await this.state.update({ idMapping });
    }

    // Emit batch phase complete for implementation
    this._emitEvent('task:batch:phase:complete', {
      step: stepName,
      phase: 'implementation',
      tasksCreated: requirements.length,
      timestamp: Date.now(),
    });

    // Calculate statistics by pattern type
    const frs = requirements.filter(r => r.taskPattern === 'tdd_chain' || !r.taskPattern);
    const verificationNFRs = requirements.filter(r => r.taskPattern === 'verification_gate');
    const aggregationTasks = requirements.filter(r => r.taskPattern === 'aggregation_task');
    const configTasks = requirements.filter(r => r.taskPattern === 'configuration_task');
    const directTasks = requirements.filter(r => r.taskPattern === 'direct_task');

    // Count fused vs full chains
    const trivialFRs = frs.filter(r => r.estimatedHours < 0.5 && (r.files || []).length <= 1);
    const complexFRs = frs.filter(r => !trivialFRs.includes(r));

    // Count full-stack USs that will be exploded into backend+frontend+integration
    const fullStackUSs = aggregationTasks.filter(r => this.isFullStackUS(r));
    const fullStackExtraSubtasks = fullStackUSs.length * 6; // backend(3) + frontend(3)

    const tddSubtasks = trivialFRs.length * 3 + complexFRs.length * 4 + aggregationTasks.length * 3 + configTasks.length * 2 + fullStackExtraSubtasks;
    // Direct tasks emit exactly one bead per requirement — no phase synthesis.
    // Beads per pattern: fr trivial=3, fr complex=4, agg=3, config=2, direct=1, verification=1.
    const totalTasks = tddSubtasks + directTasks.length + verificationNFRs.length;

    console.log(`  ✓ Created all tasks deterministically (type-aware)`);
    if (skipDiscovery) {
      console.log(`    - 0 discovery tasks (skipped - greenfield optimization)`);
    } else {
      console.log(`    - ${requirements.length} discovery tasks`);
    }
    console.log(`    - ${trivialFRs.length} trivial FRs with fused chains (3 sub-tasks: RED→GREEN→SYNTAX)`);
    console.log(`    - ${complexFRs.length} complex FRs with full TDD chains (4 sub-tasks)`);
    console.log(`    - ${aggregationTasks.length} User Stories with integration chains (3 sub-tasks each)`);
    if (fullStackUSs.length > 0) {
      console.log(`    - ${fullStackUSs.length} full-stack USs exploded into backend + frontend + integration (+6 sub-tasks each)`);
    }
    console.log(`    - ${configTasks.length} Config tasks with fused chains (2 sub-tasks: APPLY→SYNTAX)`);
    if (verificationNFRs.length > 0) {
      console.log(`    - ${verificationNFRs.length} Verification NFRs as gates`);
    }
    console.log(`    - ${tddSubtasks} total TDD/Integration sub-tasks`);
    console.log(`    - ${totalTasks} total beads`);

    // Resource estimate
    const worktreeCount = requirements.filter(r => r.needsWorktree !== false).length;
    console.log(`    - ~${worktreeCount} worktrees needed (vs ${requirements.length} in old model)`);

    // Emit task batch complete
    this._emitEvent('task:batch:complete', {
      step: stepName,
      discoveryTasks: skipDiscovery ? 0 : requirements.length,
      frChains: frs.length,
      aggregationChains: aggregationTasks.length,
      fullStackUSs: fullStackUSs.length,
      configChains: configTasks.length,
      verificationNFRs: verificationNFRs.length,
      tddSubtasks: tddSubtasks,
      totalTasks: totalTasks,
      estimatedWorktrees: worktreeCount,
      timestamp: Date.now(),
    });

    // Create LINT gate after all SYNTAX tasks
    await this.createLintGate(state);

    // Emit phase complete
    this._emitEvent('phase:complete', {
      step: stepName,
      phase: 'task-creation',
      nextStep: 'verify',
      timestamp: Date.now(),
    });

    await this.state.update({
      currentStep: 'verify',
    });
  }

  /**
   * Step 5: Create LINT gate task that runs after all SYNTAX tasks complete
   */
  async createLintGate(state) {
    const stepName = 'create-lint-gate';
    console.log('[Step 5] Creating LINT gate...');

    // Emit phase start
    this._emitEvent('phase:start', {
      step: stepName,
      phase: 'lint-gate',
      timestamp: Date.now(),
    });

    const { idMapping } = state;

    // Collect all SYNTAX task IDs from all patterns
    const syntaxTaskIds = Object.keys(idMapping).filter((localId) =>
      localId.endsWith('-syntax'),
    );

    // Also collect verification gates that need to complete before final lint
    const verificationGateIds = Object.keys(idMapping).filter((localId) =>
      localId.includes('-gate'),
    );

    if (syntaxTaskIds.length === 0) {
      console.log('  No SYNTAX tasks found, skipping LINT gate');

      // Emit phase skip
      this._emitEvent('phase:skip', {
        step: stepName,
        phase: 'lint-gate',
        reason: 'no-syntax-tasks',
        timestamp: Date.now(),
      });

      return;
    }

    console.log(`  Found ${syntaxTaskIds.length} SYNTAX tasks`);

    // Resolve SYNTAX task dependencies
    const syntaxBlockers = syntaxTaskIds.map((localId) => {
      const realId = idMapping[localId];
      if (!realId)
        throw new Error(`SYNTAX task ${localId} not found in idMapping`);
      return realId;
    });

    // Create LINT gate task
    const lintGateTask = {
      localId: 'lint-gate',
      title: 'LINT GATE: Code Quality Review',
      description: `## Scope
Comprehensive PROJECT-LEVEL Biome linting on ALL source files.

## Difference from SYNTAX phases
- **SYNTAX phases** (per-task): Run on files modified by each specific task
  - Fast feedback during development
  - Catches errors in isolated changes

- **LINT GATE** (project-level): Runs on ENTIRE src/ directory
  - Ensures global consistency across all changes
  - Validates cross-file dependencies
  - Final quality checkpoint before merge

## Why both are needed
- SYNTAX catches errors early (fail fast)
- LINT-GATE ensures holistic quality (comprehensive)

## Command
\`\`\`bash
# Run the project's linter on all source files.
# Inspect the project for linting configuration and run the appropriate command.
# Examples: npx biome lint --write src/, npx eslint --fix src/, go vet ./...
\`\`\`

## Acceptance Criteria
- Biome passes with exit code 0
- All safe fixes applied automatically
- No remaining lint errors

## Failure Handling
If this task fails, a 'Fix All Lint Errors' task will be created.
If that fix task also fails, escalation will be required.`,
      scope: 'project-level',
      level: 4,
      dependsOn: syntaxTaskIds,
      priority: 1,
      type: 'task',
      labels: ['lint-gate', 'quality-gate', 'blocks-qa'],
      metadata: {
        gate_type: 'lint',
        scope: 'all-implementation',
      },
    };

    const lintGateId = this.executor.createTask(lintGateTask, syntaxBlockers);

    // Emit task gate created
    this._emitEvent('task:gate:created', {
      step: stepName,
      gateType: 'lint',
      gateId: lintGateId,
      blockedByCount: syntaxTaskIds.length,
      timestamp: Date.now(),
    });

    console.log(
      `  ✓ LINT gate created: ${lintGateId} (blocked by ${syntaxTaskIds.length} SYNTAX tasks)`,
    );

    idMapping['lint-gate'] = lintGateId;

    // Emit phase complete
    this._emitEvent('phase:complete', {
      step: stepName,
      phase: 'lint-gate',
      nextStep: 'verify',
      timestamp: Date.now(),
    });

    // Save state
    await this.state.update({ idMapping });

    // Emit state save
    this._emitEvent('state:save', {
      step: stepName,
      timestamp: Date.now(),
    });
  }

  /**
   * Step 6: Verify and Summarize
   */
  async verifyAndSummarize(state) {
    const stepName = 'verify';
    console.log('[Step 6] Verifying task structure...');

    // Emit phase start
    this._emitEvent('phase:start', {
      step: stepName,
      phase: 'verification',
      timestamp: Date.now(),
    });

    // Verify approval gate is ready (and blocking other tasks)
    const readyTasks = this.executor.getReadyTasks();
    const approvalId = state.idMapping.approvalGate;

    // Check that approval gate is in ready list
    const approvalReady = readyTasks.some((task) => task.id === approvalId);

    if (!approvalReady) {
      const error = new Error('Expected approval gate to be ready');
      this._emitEvent('error:occurred', {
        step: stepName,
        error: error.message,
        timestamp: Date.now(),
      });
      throw error;
    }

    // Count tasks created
    const discoveryCount = Object.keys(state.idMapping).filter((k) =>
      k.startsWith('discovery-'),
    ).length;
    const implementCount = Object.keys(state.idMapping).filter(
      (k) =>
        k.startsWith('implement-') &&
        !k.includes('-red') &&
        !k.includes('-green') &&
        !k.includes('-refactor') &&
        !k.includes('-syntax'),
    ).length;
    const tddCount = Object.keys(state.idMapping).filter(
      (k) =>
        k.includes('-red') ||
        k.includes('-green') ||
        k.includes('-refactor') ||
        k.includes('-syntax'),
    ).length;

    console.log(`  ✓ Approval gate ready: ${approvalId}`);
    console.log(`  ✓ Created ${discoveryCount} discovery tasks`);
    console.log(`  ✓ Created ${implementCount} implementation tasks`);
    console.log(`  ✓ Created ${tddCount} TDD sub-tasks`);

    // Verify that main implementation tasks are NOT ready (they should be blocked)
    const implementIds = Object.keys(state.idMapping)
      .filter((k) => k.startsWith('implement-') && !k.includes('-'))
      .map((k) => state.idMapping[k]);

    const blockedImplementTasks = implementIds.filter(
      (id) => !readyTasks.some((t) => t.id === id),
    );

    console.log(
      `  ✓ ${blockedImplementTasks.length}/${implementIds.length} implementation tasks blocked`,
    );

    // Verify no duplicates by checking title uniqueness
    const titles = new Set();
    const duplicates = [];
    for (const req of state.analysis?.requirements || []) {
      const discoveryTitle = `Discovery: ${req.title}`;
      const implementTitle = req.title;

      if (titles.has(discoveryTitle)) duplicates.push(discoveryTitle);
      if (titles.has(implementTitle)) duplicates.push(implementTitle);

      titles.add(discoveryTitle);
      titles.add(implementTitle);
    }

    if (duplicates.length > 0) {
      console.warn(
        `  ⚠ Found ${duplicates.length} potential duplicate titles:`,
        duplicates,
      );
    } else {
      console.log('  ✓ No duplicate task titles detected');
    }

    console.log('  ✓ Only approval gate is ready (all others blocked)');

    // Emit verification complete
    this._emitEvent('verification:complete', {
      step: stepName,
      approvalGateReady: approvalReady,
      discoveryTasks: discoveryCount,
      implementationTasks: implementCount,
      tddSubtasks: tddCount,
      blockedImplementationTasks: blockedImplementTasks.length,
      duplicateWarnings: duplicates.length,
      timestamp: Date.now(),
    });

    // Emit phase complete
    this._emitEvent('phase:complete', {
      step: stepName,
      phase: 'verification',
      nextStep: 'completed',
      timestamp: Date.now(),
    });

    await this.state.update({
      currentStep: 'completed',
    });

    // Emit state save
    this._emitEvent('state:save', {
      step: stepName,
      timestamp: Date.now(),
    });
  }

  /**
   * Format task description with JSON Context + Markdown Instructions
   * Large data model definitions are moved to metadata with a reference in description
   * @param {Object} options - Formatting options
   * @returns {string} Formatted description
   */
  formatTaskDescription({ req, phase, instructions }) {
    // Build compact context for JSON (without large definitions)
    const context = {
      task: {
        id: req.originalId || req.id,
        title: req.title,
        type: req.type,
        phase: phase,
        estimated_hours: req.estimatedHours,
      },
      target: {
        files: req.files || [],
        functions: req.functions || [],
        integration_points:
          req.integrationPoints?.map((p) => ({
            id: p.pointNumber,
            location: this.extractLocationFromPoint(p),
          })) || [],
      },
      data: {
        // Only names and types in description - full definitions in metadata
        models:
          req.dataModels?.map((m) => ({
            name: m.name,
            type: m.type,
          })) || [],
      },
      criteria: req.acceptanceCriteria || [],
      configuration: req.configuration || [],
      dependencies: req.dependencies || [],
    };

    return `## Context

**Note: Full data model definitions are available in the task metadata under "data_models" field.**

\`\`\`json
${JSON.stringify(context, null, 2)}
\`\`\`

## Instructions

${instructions}

## PRD Description

${req.description}`;
  }

  /**
   * Format description for tracker/parent tasks (requirements only, no implementation work)
   * @param {Object} options - Formatting options
   * @param {Object} options.req - Requirement object
   * @param {string} options.instructions - Tracking instructions
   * @returns {string} Formatted tracker description
   */
  /**
   * Verification Command contract (FR-001, executable-acceptance PRD):
   * every synthesized bead carries metadata.verification_command plus a
   * **Verification Command:** fenced block in its description.
   *   - Test Command present  → verification_command equals it byte-for-byte.
   *   - Test Command missing  → Build Command + non-vacuity wrapper tail.
   *   - Neither present       → the non-vacuity wrapper alone (still non-empty).
   */
  buildVerificationCommand(testCommand, buildCommand) {
    const NON_VACUITY_TAIL =
      'echo "verification: non-vacuity gate — command completed with exit 0"';
    const test = (testCommand || '').trim();
    if (test) return test;
    const build = (buildCommand || '').trim();
    if (build) return `${build} && ${NON_VACUITY_TAIL}`;
    return NON_VACUITY_TAIL;
  }

  /** Machine-parseable fenced block rendering the verification command. */
  verificationCommandBlock(command) {
    return `\n\n**Verification Command:**\n\n\`\`\`sh\n${command}\n\`\`\``;
  }

  /** Attach the verification_command contract (metadata + description block) to a bead. */
  applyVerificationContract(task, testCommand, buildCommand) {
    const command = this.buildVerificationCommand(testCommand, buildCommand);
    task.metadata = { ...task.metadata, verification_command: command };
    task.description = (task.description || '') + this.verificationCommandBlock(command);
    return command;
  }

  formatTrackerDescription({ req, instructions }) {
    // Build context for tracker tasks (minimal, just requirement context)
    const context = {
      requirement: {
        id: req.originalId || req.id,
        title: req.title,
        type: req.type,
        estimated_hours: req.estimatedHours,
      },
      files: req.files || [],
      functions: req.functions || [],
      criteria: req.acceptanceCriteria || [],
    };

    return `## Requirement Context

\`\`\`json
${JSON.stringify(context, null, 2)}
\`\`\`

${instructions}

## PRD Description

${req.description}`;
  }

  /**
   * Extract file:function location from integration point
   * @param {Object} point - Integration point
   * @returns {string} Location string
   */
  extractLocationFromPoint(point) {
    const match = point.details.match(/`?([\w/]+\.\w+):(\w+)\(\)`?/);
    if (match) {
      return `${match[1]}:${match[2]}()`;
    }
    return point.title;
  }

  /**
   * Get development constraints for Node.js/JS/TS projects
   * @param {Object} parentTask - Parent task object
   * @param {string} phase - TDD phase (red, green, refactor, syntax)
   * @returns {string} Development constraints block
   */
  getDevelopmentConstraints() {
    return `
### Development Constraints
- All floating-point comparisons MUST use \`toBeCloseTo()\`, NOT \`toBe()\`. Applies to: sqrt, log, divide, pow, sin, cos, tan results
- Lint must pass before bead is complete
- Test output must show 0 failures
- Tooling prerequisites verified: node, pnpm in PATH`;
  }

  /**
   * Format TDD subtask description with inherited criteria
   * @param {Object} options - Formatting options
   * @returns {string} Formatted description
   */
  formatTDDDescription({
    phase,
    cleanTitle,
    parentCriteria,
    parentFiles,
    parentFunctions,
    specificInstructions,
  }) {
    const context = {
      task: {
        title: cleanTitle,
        phase: phase,
        parent_phase: 'tdd',
      },
      target: {
        files: parentFiles,
        functions: parentFunctions,
      },
      criteria: {
        parent: parentCriteria,
        phase_specific: this.getPhaseSpecificCriteria(phase),
      },
    };

    // Extract exact error strings from criteria for injection into agent prompt
    const exactErrors = (parentCriteria || [])
      .filter(c => typeof c === 'string' && (c.includes('throws') || c.includes('Error')))
      .map(c => {
        const m = c.match(/["']([^"']+)["']/);
        return m ? m[1] : null;
      })
      .filter(Boolean);

    const errorBlock = exactErrors.length > 0
      ? `\n\n### Exact Error Strings (do NOT paraphrase — use these literals)\n${exactErrors.map(e => `- "${e}"`).join('\n')}`
      : '';

    const definitionOfDone = `\n\n### Definition of Done\n- [ ] All target files are non-empty and contain no placeholder comments (e.g. "/\/\/ Placeholder/", "/\/\/ TODO/", "/\/\/ FIXME/")\n- [ ] 
 All acceptance criteria from the parent requirement are satisfied\n- [ ] The implementation is importable/resolvable from downstream consumers\n- [ ] No 
 
 
 
 
 
  types are introduced`;

    return `## Context\n\n\`\`\`json\n${JSON.stringify(context, null, 2)}\n\`\`\`\n\n## Instructions\n\n${specificInstructions}${errorBlock}${this.getDevelopmentConstraints()}${definitionOfDone}`;
  }

  /**
   * Get phase-specific TDD criteria
   * @param {string} phase - TDD phase (red, green, refactor, syntax)
   * @returns {Array} Phase-specific criteria
   */
  getPhaseSpecificCriteria(phase) {
    const criteria = {
      red: [
        'Tests cover all parent acceptance criteria',
        'Tests fail initially (RED phase)',
        'Coverage target: measure and report coverage for new code',
      ],
      green: [
        'All tests pass (GREEN phase)',
        'Implementation satisfies parent criteria',
        'No premature refactoring',
        'Coverage: report coverage percentage for new implementation code',
      ],
      refactor: [
        'Code is clean and maintainable',
        'All tests still pass',
        'Better naming, no duplication',
        'Improved readability',
        'Performance: verify no regressions if benchmarks exist',
      ],
      syntax: [
        'Lint and type-check pass with no errors',
        'No syntax errors in modified files',
        'Code follows project style guidelines',
        'Documentation: verify JSDoc/doc comments exist on all public APIs',
      ],
    };
    return criteria[phase] || [];
  }

  /**
   * Generate TDD sub-tasks for an implementation task
   */
  generateTDDSubTasks(parentTask, redDependencies = []) {
    const baseId = parentTask.localId;
    // Use normalizeTitle for consistent cleanup across all titles
    const cleanTitle = this.normalizeTitle(parentTask.title)
      .replace(/^FR-\d+[a-z]?:\s*/i, '') // Remove FR-XXX: prefix
      .replace(/^NFR-\d+[a-z]?:\s*/i, '') // Remove NFR-XXX: prefix
      .replace(/^US-\d+[a-z]?:\s*/i, '') // Remove US-XXX: prefix
      .replace(/^Implement\s+/i, '') // Remove leading "Implement"
      .trim();

    // Extract parent criteria from metadata or parse from parent description
    const parentCriteria = parentTask.metadata?.parent_criteria || [];
    const parentFiles = parentTask.metadata?.files || [];
    const parentFunctions = parentTask.metadata?.functions || [];
    const estimatedHours = parentTask.metadata?.estimated_hours || 2;

    // PHASE FUSION: Skip REFACTOR for trivial operations
    // Heuristic: <0.5h, single file, no complex dependencies
    const isTrivial = estimatedHours < 0.5 && parentFiles.length <= 1;
    if (isTrivial) {
      console.log(`        ⚡ Phase fusion: skipping REFACTOR for ${cleanTitle} (trivial: ${estimatedHours}h, ${parentFiles.length} file)`);
    }

    const testCommand = parentTask.metadata?.test_command || '';
    const buildCommand = parentTask.metadata?.build_command || '';
    const testCmdLine = testCommand ? '\n- Run the exact test command: ' + testCommand + ' and confirm exit code 0' : '';
    const buildCmdLine = buildCommand ? '\n- Run the exact build command: ' + buildCommand + ' and confirm exit code 0' : '';

    // Verification Command contract: propagated to every phase bead below.

    const tasks = [
      {
        localId: `${baseId}-red`,
        title: `RED: Write tests for ${cleanTitle}`,
        description: this.formatTDDDescription({
          phase: 'red',
          cleanTitle,
          parentCriteria,
          parentFiles,
          parentFunctions,
          specificInstructions:
            'Write failing tests that cover all acceptance criteria below.',
        }),
        level: parentTask.level,
        dependsOn: redDependencies,
        priority: parentTask.priority,
        type: 'task',
        labels: [...new Set([...parentTask.labels, 'tdd-red', 'testing'])],
        metadata: {
          tdd_phase: 'red',
          parent_task: baseId,
        },
      },
      {
        localId: `${baseId}-green`,
        title: `GREEN: Implement ${cleanTitle}`,
        description: this.formatTDDDescription({
          phase: 'green',
          cleanTitle,
          parentCriteria,
          parentFiles,
          parentFunctions,
          specificInstructions:
            `Implement the functionality to make all tests pass. Focus on correctness over elegance.\n\nAfter implementation:${testCmdLine}${buildCmdLine}\n- Run coverage reporting on the new code and report the coverage percentage\n- Ensure coverage is reasonable for the new implementation (if tests are comprehensive, coverage should reflect that)\n- Verify every target file is non-empty and contains no placeholder comments (e.g. "// Placeholder", "// TODO", "// FIXME")`,
        }),
        level: parentTask.level,
        dependsOn: [`${baseId}-red`],
        priority: parentTask.priority,
        type: 'task',
        labels: [
          ...new Set([...parentTask.labels, 'tdd-green', 'implementation']),
        ],
        metadata: {
          tdd_phase: 'green',
          parent_task: baseId,
        },
      },
    ];

    // Add REFACTOR phase (skipped for trivial operations)
    if (!isTrivial) {
      tasks.push({
        localId: `${baseId}-refactor`,
        title: `REFACTOR: Optimize ${cleanTitle}`,
        description: this.formatTDDDescription({
          phase: 'refactor',
          cleanTitle,
          parentCriteria,
          parentFiles,
          parentFunctions,
          specificInstructions:
            'Refactor the implementation while keeping all tests passing. Improve naming, remove duplication, enhance readability.\n\nAfter refactoring:\n1. Run all tests to confirm they still pass\n2. If benchmarks exist for this functionality, run them to verify no performance regressions\n3. Verify the code remains easy to understand and maintain',
        }),
        level: parentTask.level,
        dependsOn: [`${baseId}-green`],
        priority: parentTask.priority,
        type: 'task',
        labels: [...new Set([...parentTask.labels, 'tdd-refactor', 'cleanup'])],
        metadata: {
          tdd_phase: 'refactor',
          parent_task: baseId,
        },
      });
    }

    // SYNTAX phase depends on REFACTOR (if present) or GREEN (if REFACTOR skipped)
    const syntaxDependsOn = isTrivial ? [`${baseId}-green`] : [`${baseId}-refactor`];
    const syntaxTestCmd = testCommand ? `\n- Run \`${testCommand}\` and confirm exit code 0` : '';
    const syntaxBuildCmd = buildCommand ? `\n- Run \`${buildCommand}\` and confirm exit code 0` : '';
    tasks.push({
      localId: `${baseId}-syntax`,
      title: `SYNTAX: Check ${cleanTitle}`,
      description: this.formatTDDDescription({
        phase: 'syntax',
        cleanTitle,
        parentCriteria,
        parentFiles,
        parentFunctions,
        specificInstructions: `Run linting and type-checking on modified files. Fix any errors or warnings.\n\nAlso verify:\n1. All public functions/methods/types have adequate documentation (JSDoc, Go doc, docstrings, etc.)\n2. Type checking passes with no errors\n3. Run biome format --write (or the project's formatter) on all modified files to ensure consistent formatting\n4. Code follows project style guidelines${syntaxTestCmd}${syntaxBuildCmd}\n5. Verify every target file is non-empty and contains no placeholder comments (e.g. "// Placeholder", "// TODO", "// FIXME")`,
      }),
      level: parentTask.level,
      dependsOn: syntaxDependsOn,
      priority: parentTask.priority,
      type: 'task',
      labels: [
        ...new Set([
          ...parentTask.labels,
          'tdd-syntax',
          'quality',
          'attempt-syntax-lint-max-5',
          'attempt-syntax-lint-retries-0',
        ]),
      ],
      metadata: {
        tdd_phase: 'syntax',
        parent_task: baseId,
        syntax_check: true,
        files_to_check: parentFiles,
        max_retries: 5,
        current_retry: 0,
      },
    });

    for (const sub of tasks) {
      this.applyVerificationContract(sub, testCommand, buildCommand);
    }

    return tasks;
  }

  /**
   * Create aggregation task (for User Stories)
   * These integrate multiple FRs into user-facing features
   */
  /**
   * Detect if a User Story implies full-stack (backend + frontend)
   * based on keywords in title, description, and acceptance criteria.
   */
  isFullStackUS(req) {
    const text = `${req.title} ${req.description || ''} ${(req.acceptanceCriteria || []).join(' ')}`.toLowerCase();
    const backendKeywords = ['api', 'endpoint', 'route', 'express', 'backend', 'handler', 'controller', 'server', 'post ', 'get '];
    const frontendKeywords = ['web', 'component', 'ui', 'frontend', 'react', 'vue', 'render', 'display', 'html', 'dom', 'page'];
    const hasBackend = backendKeywords.some(kw => text.includes(kw));
    const hasFrontend = frontendKeywords.some(kw => text.includes(kw));
    return hasBackend && hasFrontend;
  }

  async createAggregationTask(req, parentTask, parentId, state, aggregatedSyntaxIds = []) {
    const { idMapping } = state;
    const stepName = 'create-tasks';

    const baseId = parentTask.localId;
    const cleanTitle = this.normalizeTitle(req.title)
      .replace(/^US-\d+[a-z]?:\s*/i, '')
      .trim();

    const testCommand = req.testCommand || '';
    const buildCommand = req.buildCommand || '';
    const testCmdLine = testCommand ? '\n- Run the exact test command: ' + testCommand + ' and confirm exit code 0' : '';
    const buildCmdLine = buildCommand ? '\n- Run the exact build command: ' + buildCommand + ' and confirm exit code 0' : '';

    // Verification Command contract: every aggregation bead carries the command.
    const vc = this.buildVerificationCommand(testCommand, buildCommand);
    const _aggCreateTask = (task, blockers) => {
      this.applyVerificationContract(task, testCommand, buildCommand);
      return this.executor.createTask(task, blockers);
    };

    // FULL-STACK EXPLOSION: If US implies both backend and frontend,
    // create separate TDD chains for backend and frontend BEFORE integration.
    // This prevents one agent from being overwhelmed with backend + frontend + wiring.
    const isFullStack = this.isFullStackUS(req);
    let backendSyntaxId = null;
    let frontendSyntaxId = null;

    if (isFullStack) {
      console.log(`        🧨 Full-stack US detected: exploding into backend + frontend + integration chains`);

      // Infer backend files from criteria mentioning API/backend
      const backendFiles = (req.files || []).filter(f =>
        f.includes('api') || f.includes('server') || f.includes('route') || f.includes('controller')
      );
      const backendCriteria = (req.acceptanceCriteria || []).filter(c =>
        /api|endpoint|route|express|server|post |get |handler|controller/i.test(c)
      );
      const backendTitle = `API Backend for ${cleanTitle}`;
      const backendBaseId = `${baseId}-backend`;

      // Backend RED
      const backendRedTask = {
        localId: `${backendBaseId}-red`,
        title: `RED: Write tests for ${backendTitle}`,
        description: this.formatTDDDescription({
          phase: 'red',
          cleanTitle: backendTitle,
          parentCriteria: backendCriteria,
          parentFiles: backendFiles.length > 0 ? backendFiles : ['apps/api/src/**/*.ts'],
          parentFunctions: req.functions || [],
          specificInstructions: `Write failing tests for the backend API. Tests must exercise real endpoints and fail if routes or handlers are missing.`,
        }),
        level: parentTask.level,
        dependsOn: aggregatedSyntaxIds,
        priority: parentTask.priority,
        type: 'task',
        labels: [...new Set([...parentTask.labels, 'tdd-red', 'backend-test'])],
        metadata: { tdd_phase: 'red', parent_task: backendBaseId },
      };
      const backendRedBlockers = aggregatedSyntaxIds.map(localId => idMapping[localId]).filter(Boolean);
      const backendRedId = _aggCreateTask(backendRedTask, backendRedBlockers);
      console.log(`        ✓ ${backendRedTask.localId} → ${backendRedId}`);
      idMapping[backendRedTask.localId] = backendRedId;

      // Backend GREEN
      const backendGreenTask = {
        localId: `${backendBaseId}-green`,
        title: `GREEN: Implement ${backendTitle}`,
        description: this.formatTDDDescription({
          phase: 'green',
          cleanTitle: backendTitle,
          parentCriteria: backendCriteria,
          parentFiles: backendFiles.length > 0 ? backendFiles : ['apps/api/src/**/*.ts'],
          parentFunctions: req.functions || [],
          specificInstructions: `Implement the backend API (routes, handlers, controllers). Make all backend tests pass.` + testCmdLine + buildCmdLine,
        }),
        level: parentTask.level,
        dependsOn: [`${backendBaseId}-red`],
        priority: parentTask.priority,
        type: 'task',
        labels: [...new Set([...parentTask.labels, 'tdd-green', 'backend'])],
        metadata: { tdd_phase: 'green', parent_task: backendBaseId },
      };
      const backendGreenId = _aggCreateTask(backendGreenTask, [backendRedId]);
      console.log(`        ✓ ${backendGreenTask.localId} → ${backendGreenId}`);
      idMapping[backendGreenTask.localId] = backendGreenId;

      // Backend SYNTAX
      const backendSyntaxTask = {
        localId: `${backendBaseId}-syntax`,
        title: `SYNTAX: Check ${backendTitle}`,
        description: this.formatTDDDescription({
          phase: 'syntax',
          cleanTitle: backendTitle,
          parentCriteria: backendCriteria,
          parentFiles: backendFiles.length > 0 ? backendFiles : ['apps/api/src/**/*.ts'],
          parentFunctions: req.functions || [],
          specificInstructions: `Run linting and type-checking on backend code. Fix any errors.` + testCmdLine + buildCmdLine,
        }),
        level: parentTask.level,
        dependsOn: [`${backendBaseId}-green`],
        priority: parentTask.priority,
        type: 'task',
        labels: [...new Set([...parentTask.labels, 'tdd-syntax', 'backend-quality'])],
        metadata: { tdd_phase: 'syntax', parent_task: backendBaseId },
      };
      backendSyntaxId = _aggCreateTask(backendSyntaxTask, [backendGreenId]);
      console.log(`        ✓ ${backendSyntaxTask.localId} → ${backendSyntaxId}`);
      idMapping[backendSyntaxTask.localId] = backendSyntaxId;

      // Infer frontend files from criteria mentioning web/component
      const frontendFiles = (req.files || []).filter(f =>
        f.includes('web') || f.includes('component') || f.includes('ui') || f.includes('frontend')
      );
      const frontendCriteria = (req.acceptanceCriteria || []).filter(c =>
        /web|component|ui|frontend|render|display|html|dom|page/i.test(c)
      );
      const frontendTitle = `Web Frontend for ${cleanTitle}`;
      const frontendBaseId = `${baseId}-frontend`;

      // Frontend RED
      const frontendRedTask = {
        localId: `${frontendBaseId}-red`,
        title: `RED: Write tests for ${frontendTitle}`,
        description: this.formatTDDDescription({
          phase: 'red',
          cleanTitle: frontendTitle,
          parentCriteria: frontendCriteria,
          parentFiles: frontendFiles.length > 0 ? frontendFiles : ['apps/web/src/**/*.ts', 'apps/web/src/**/*.tsx'],
          parentFunctions: req.functions || [],
          specificInstructions: `Write failing tests for the web frontend. Tests must exercise real components and fail if UI is missing.`,
        }),
        level: parentTask.level,
        dependsOn: aggregatedSyntaxIds,
        priority: parentTask.priority,
        type: 'task',
        labels: [...new Set([...parentTask.labels, 'tdd-red', 'frontend-test'])],
        metadata: { tdd_phase: 'red', parent_task: frontendBaseId },
      };
      const frontendRedBlockers = aggregatedSyntaxIds.map(localId => idMapping[localId]).filter(Boolean);
      const frontendRedId = _aggCreateTask(frontendRedTask, frontendRedBlockers);
      console.log(`        ✓ ${frontendRedTask.localId} → ${frontendRedId}`);
      idMapping[frontendRedTask.localId] = frontendRedId;

      // Frontend GREEN
      const frontendGreenTask = {
        localId: `${frontendBaseId}-green`,
        title: `GREEN: Implement ${frontendTitle}`,
        description: this.formatTDDDescription({
          phase: 'green',
          cleanTitle: frontendTitle,
          parentCriteria: frontendCriteria,
          parentFiles: frontendFiles.length > 0 ? frontendFiles : ['apps/web/src/**/*.ts', 'apps/web/src/**/*.tsx'],
          parentFunctions: req.functions || [],
          specificInstructions: `Implement the web frontend (components, UI logic). Make all frontend tests pass.` + testCmdLine + buildCmdLine,
        }),
        level: parentTask.level,
        dependsOn: [`${frontendBaseId}-red`],
        priority: parentTask.priority,
        type: 'task',
        labels: [...new Set([...parentTask.labels, 'tdd-green', 'frontend'])],
        metadata: { tdd_phase: 'green', parent_task: frontendBaseId },
      };
      const frontendGreenId = _aggCreateTask(frontendGreenTask, [frontendRedId]);
      console.log(`        ✓ ${frontendGreenTask.localId} → ${frontendGreenId}`);
      idMapping[frontendGreenTask.localId] = frontendGreenId;

      // Frontend SYNTAX
      const frontendSyntaxTask = {
        localId: `${frontendBaseId}-syntax`,
        title: `SYNTAX: Check ${frontendTitle}`,
        description: this.formatTDDDescription({
          phase: 'syntax',
          cleanTitle: frontendTitle,
          parentCriteria: frontendCriteria,
          parentFiles: frontendFiles.length > 0 ? frontendFiles : ['apps/web/src/**/*.ts', 'apps/web/src/**/*.tsx'],
          parentFunctions: req.functions || [],
          specificInstructions: `Run linting and type-checking on frontend code. Fix any errors.` + testCmdLine + buildCmdLine,
        }),
        level: parentTask.level,
        dependsOn: [`${frontendBaseId}-green`],
        priority: parentTask.priority,
        type: 'task',
        labels: [...new Set([...parentTask.labels, 'tdd-syntax', 'frontend-quality'])],
        metadata: { tdd_phase: 'syntax', parent_task: frontendBaseId },
      };
      frontendSyntaxId = _aggCreateTask(frontendSyntaxTask, [frontendGreenId]);
      console.log(`        ✓ ${frontendSyntaxTask.localId} → ${frontendSyntaxId}`);
      idMapping[frontendSyntaxTask.localId] = frontendSyntaxId;
    }

    // Build integration dependencies: aggregated FRs + backend syntax + frontend syntax
    const integrationRedDeps = [...aggregatedSyntaxIds];
    if (backendSyntaxId) integrationRedDeps.push(`${baseId}-backend-syntax`);
    if (frontendSyntaxId) integrationRedDeps.push(`${baseId}-frontend-syntax`);

    // RED: Integration tests — depends on aggregated FR SYNTAX tasks + backend/frontend syntax
    const redTask = {
      localId: `${baseId}-red`,
      title: `RED: Write integration tests for ${cleanTitle}`,
      description: this.formatTDDDescription({
        phase: 'red',
        cleanTitle,
        parentCriteria: req.acceptanceCriteria,
        parentFiles: req.files,
        parentFunctions: req.functions,
        specificInstructions: `Write integration tests that verify the aggregated features work together. Tests must exercise real code, not mocks, and must fail if any aggregated FR is missing or broken.` +
          (isFullStack ? '\n\nCRITICAL: This is a WIRING-ONLY task. The backend API and frontend component are implemented in separate beads. This bead ONLY writes tests that verify they work together (e.g., end-to-end test calling API through frontend).' : '') +
          `\n\nAggregated FRs: ${(req.dependsOn || []).join(', ')}`,
      }),
      level: parentTask.level,
      dependsOn: integrationRedDeps,
      priority: parentTask.priority,
      type: 'task',
      labels: [...new Set([...parentTask.labels, 'tdd-red', 'integration-test'])],
      metadata: {
        tdd_phase: 'red',
        parent_task: baseId,
        aggregated_frs: req.dependsOn,
      },
    };

    const redBlockers = integrationRedDeps.map(localId => idMapping[localId]).filter(Boolean);
    const redId = _aggCreateTask(redTask, redBlockers);
    this._emitEvent('task:subtask:created', {
      step: stepName,
      parentLocalId: baseId,
      localId: redTask.localId,
      taskId: redId,
      subtaskType: 'red',
      timestamp: Date.now(),
    });
    console.log(`        ✓ ${redTask.localId} → ${redId}`);
    idMapping[redTask.localId] = redId;

    // GREEN: Integration implementation (WIRING ONLY for full-stack)
    const greenTask = {
      localId: `${baseId}-green`,
      title: `GREEN: Integrate ${cleanTitle}`,
      description: this.formatTDDDescription({
        phase: 'green',
        cleanTitle,
        parentCriteria: req.acceptanceCriteria,
        parentFiles: req.files,
        parentFunctions: req.functions,
        specificInstructions: (isFullStack
          ? `Wire the backend API and frontend component together. This is a WIRING-ONLY task — do NOT reimplement the API or frontend.\n\nTasks:\n1. Ensure the frontend can import and call the backend API\n2. Create barrel exports if needed so downstream consumers can import\n3. Verify end-to-end tests pass`
          : `Implement the integration layer (e.g., barrel exports, API facade, UI wiring). Every target file must be non-empty and importable from the project entry point.`
        ) + testCmdLine + buildCmdLine + `
- Verify every target file is non-empty and contains no placeholder comments (e.g. "// Placeholder", "// TODO", "// FIXME")
- Ensure all aggregated FRs are importable/resolvable from this integration layer

Aggregated FRs: ${(req.dependsOn || []).join(', ')}`,
      }),
      level: parentTask.level,
      dependsOn: [`${baseId}-red`],
      priority: parentTask.priority,
      type: 'task',
      labels: [...new Set([...parentTask.labels, 'tdd-green', 'integration'])],
      metadata: {
        tdd_phase: 'green',
        parent_task: baseId,
        aggregated_frs: req.dependsOn,
      },
    };

    const greenId = _aggCreateTask(greenTask, [redId]);
    this._emitEvent('task:subtask:created', {
      step: stepName,
      parentLocalId: baseId,
      localId: greenTask.localId,
      taskId: greenId,
      subtaskType: 'green',
      timestamp: Date.now(),
    });
    console.log(`        ✓ ${greenTask.localId} → ${greenId}`);
    idMapping[greenTask.localId] = greenId;

    // SYNTAX: Integration lint check
    const syntaxTask = {
      localId: `${baseId}-syntax`,
      title: `SYNTAX: Check ${cleanTitle} integration`,
      description: this.formatTDDDescription({
        phase: 'syntax',
        cleanTitle,
        parentCriteria: req.acceptanceCriteria,
        parentFiles: req.files,
        parentFunctions: req.functions,
        specificInstructions: `Run linting and type-checking on integration code.` + testCmdLine + buildCmdLine + `
- Verify every target file is non-empty and contains no placeholder comments
- Confirm the full project builds and tests pass with the integration layer in place`,
      }),
      level: parentTask.level,
      dependsOn: [`${baseId}-green`],
      priority: parentTask.priority,
      type: 'task',
      labels: [...new Set([...parentTask.labels, 'tdd-syntax', 'quality'])],
      metadata: {
        tdd_phase: 'syntax',
        parent_task: baseId,
      },
    };

    const syntaxId = _aggCreateTask(syntaxTask, [greenId]);
    this._emitEvent('task:subtask:created', {
      step: stepName,
      parentLocalId: baseId,
      localId: syntaxTask.localId,
      taskId: syntaxId,
      subtaskType: 'syntax',
      timestamp: Date.now(),
    });
    console.log(`        ✓ ${syntaxTask.localId} → ${syntaxId}`);
    idMapping[syntaxTask.localId] = syntaxId;

    await this.state.update({ idMapping });
  }

  /**
   * Map a DIRECT_TASK requirement to its shape-specific acceptance line.
   * @param {Object} req - Parsed requirement (needs .type, .originalId)
   * @returns {string} Acceptance line for the deliverable shape
   */
  getDirectTaskAcceptanceLine(req) {
    const t = (req.type || '').toLowerCase();
    const orig = (req.originalId || '').toUpperCase();
    if (t.includes('test') || orig.startsWith('TEST-')) {
      return 'suite exits 0; assertion count > 0; zero skipped/todo tests';
    }
    if (t.includes('refactor') || orig.startsWith('REF-')) {
      return 'existing suite exits 0 unchanged';
    }
    if (t.includes('deletion') || orig.startsWith('DEL-')) {
      return 'suite exits 0; absence grep returns no matches';
    }
    if (t.includes('spike') || t.includes('audit') || orig.startsWith('SPIKE-') || orig.startsWith('AUDIT-')) {
      return 'findings artifact exists, non-vacuous, citations resolve where applicable';
    }
    return 'Test Command exits 0';
  }

  /**
   * DIRECT_TASK: emit exactly ONE bead per requirement — no RED/GREEN/REFACTOR/SYNTAX
   * phase synthesis. Used for docs, test-authoring, refactor, spike, deletion, audit
   * deliverables where TDD ceremony is wrong-shape overhead.
   */
  async createDirectTask(req, parentTask, parentId, state, redDeps = []) {
    const { idMapping } = state;
    const stepName = 'create-tasks';

    const cleanTitle = this.normalizeTitle(req.title);
    const acceptanceLine = this.getDirectTaskAcceptanceLine(req);
    const testCommand = (req.testCommand || '').trim();
    const buildCommand = (req.buildCommand || '').trim();

    let instructions = `## Direct Task

This is a single-bead deliverable task — no TDD phase synthesis.

Deliverable: ${cleanTitle}.

Acceptance: ${acceptanceLine}.`;

    if (testCommand) {
      instructions += `\n\nTest Command (must be run exactly as written):\n\n\`${testCommand}\``;
    }

    const directTask = {
      localId: parentTask.localId,
      title: req.title,
      description: this.formatTrackerDescription({ req, instructions }),
      level: parentTask.level,
      dependsOn: redDeps,
      priority: parentTask.priority,
      type: 'task',
      labels: [...new Set([...parentTask.labels, 'direct-task'])],
      metadata: {
        prd_requirement_id: req.id,
        task_pattern: 'direct_task',
        test_command: req.testCommand || '',
        build_command: req.buildCommand || '',
        files: req.files || [],
        parent_criteria: req.acceptanceCriteria || [],
        acceptance_line: acceptanceLine,
      },
    };
    this.applyVerificationContract(directTask, testCommand, buildCommand);

    const blockers = redDeps.map((depLocalId) => {
      const realId = idMapping[depLocalId];
      if (!realId)
        throw new Error(`Direct task dependency ${depLocalId} not found`);
      return realId;
    });

    const directId = this.executor.createTask(directTask, blockers);
    this._recordSubtaskEmission(directId, {
      step: stepName,
      parentLocalId: parentTask.localId,
      task: directTask,
      subtaskType: 'direct',
      state,
    });
    idMapping[parentTask.localId] = directId;

    await this.state.update({ idMapping });
  }

  /**
   * Record a just-created subtask bead: emit its creation event, log the
   * mapping, and register the localId → real bead id in the state idMapping.
   * Shared by every subtask-synthesis path (TDD phases, config, direct).
   */
  _recordSubtaskEmission(taskId, { step, parentLocalId, task, subtaskType, state }) {
    this._emitEvent('task:subtask:created', {
      step,
      parentLocalId,
      localId: task.localId,
      taskId,
      subtaskType,
      timestamp: Date.now(),
    });
    console.log(`        ✓ ${task.localId} → ${taskId}${subtaskType === 'direct' ? ' (direct task)' : ''}`);
    state.idMapping[task.localId] = taskId;
  }

  async createConfigurationTask(req, parentTask, parentId, state, redDeps = []) {
    const { idMapping } = state;
    const stepName = 'create-tasks';

    const baseId = parentTask.localId;
    const cleanTitle = this.normalizeTitle(req.title)
      .replace(/^NFR-\d+[a-z]?:\s*/i, '')
      .trim();

    // CONFIG FUSION: Single "Apply Config" bead instead of RED→GREEN→SYNTAX
    // Config tasks don't need pre-written tests for files that don't exist yet.
    // The agent applies config, then SYNTAX validates it.

    const isWildcardFiles = !req.files || req.files.length === 0 || req.files.every(f => f === '*' || f === 'TBD');
    const targetFiles = isWildcardFiles ? DEFAULT_CONFIG_TARGET_FILES : req.files;

    const targetFilesBlock = isWildcardFiles ? `\nTarget files (create ALL of these):\n${DEFAULT_CONFIG_TARGET_FILES.map(f => `- ${f}`).join('\n')}` : '';

    const applySpecificInstructions = `Apply the configuration for ${cleanTitle}.${targetFilesBlock}

This is an infrastructure/setup task. Create or update ONLY configuration files (e.g., package.json, tsconfig.json, turbo.json, build configs) according to the acceptance criteria. Do NOT write implementation code or tests in this task.

CRITICAL — If you create tsconfig.json that extends another file, you MUST also create that extended file. For example, if tsconfig.json contains "extends": "./tsconfig.base.json", then tsconfig.base.json MUST exist in the same directory with valid compilerOptions.

After applying:\n1. Verify the configuration files are syntactically valid (e.g., JSON parses, schema is correct)\n2. Do NOT run a full project build or test suite — config files alone should be validated\n3. Report what config files were created or modified

CRITICAL — Create or update \`.gitignore\` to exclude:
- .hydra/
- .beads/
- node_modules/
- .pi/
- .tldr/
- *.log

CRITICAL — If using Biome for linting/formatting, create \`biome.json\` with:
{
  "files": {
    "ignore": [".hydra", ".beads", "node_modules", ".git"]
  }
}
This prevents Biome from scanning worktree directories.

Then VALIDATE the configuration in the same bead (syntax gate fused into apply):
1. All config files are syntactically valid
2. Required fields are present
3. Config matches project conventions
4. Run biome format --write on all config files to ensure consistent formatting`;

    const applyTask = {
      localId: `${baseId}-apply`,
      title: `APPLY: Configure ${cleanTitle}`,
      description: this.formatTDDDescription({
        phase: 'green',
        cleanTitle,
        parentCriteria: req.acceptanceCriteria,
        parentFiles: targetFiles,
        parentFunctions: [],
        specificInstructions: applySpecificInstructions,
      }),
      level: parentTask.level,
      dependsOn: redDeps,
      priority: parentTask.priority,
      type: 'task',
      labels: [...new Set([...parentTask.labels, 'config-apply', 'configuration', 'phase:apply', 'tdd-syntax', 'quality'])],
      metadata: {
        tdd_phase: 'apply',
        parent_task: baseId,
        config_type: req.originalId,
      },
    };

    // Verification Command contract (Test Command when present, Build Command + wrapper otherwise)
    this.applyVerificationContract(applyTask, req.testCommand, req.buildCommand || parentTask.metadata?.build_command);

    const applyBlockers = redDeps.map(localId => idMapping[localId]).filter(Boolean);
    const applyId = this.executor.createTask(applyTask, applyBlockers);
    this._recordSubtaskEmission(applyId, {
      step: stepName,
      parentLocalId: baseId,
      task: applyTask,
      subtaskType: 'apply',
      state,
    });

    // SYNTAX: Validate configuration
    const syntaxTask = {
      localId: `${baseId}-syntax`,
      title: `SYNTAX: Validate ${cleanTitle} config`,
      description: this.formatTDDDescription({
        phase: 'syntax',
        cleanTitle,
        parentCriteria: req.acceptanceCriteria,
        parentFiles: targetFiles,
        parentFunctions: [],
        specificInstructions: `Validate configuration files pass linting and schema checks.\n\nVerify:\n1. All config files are syntactically valid\n2. Required fields are present\n3. Config matches project conventions\n4. Run biome format --write on all config files to ensure consistent formatting`,
      }),
      level: parentTask.level,
      dependsOn: [`${baseId}-apply`],
      priority: parentTask.priority,
      type: 'task',
      labels: [...new Set([...parentTask.labels, 'tdd-syntax', 'quality', 'phase:syntax'])],
      metadata: {
        tdd_phase: 'syntax',
        parent_task: baseId,
      },
    };

    const syntaxId = applyId;
    await this.state.update({ idMapping });
  }

  /**
   * Handle errors with retry and rollback
   */
  async handleError(error) {
    const state = await this.state.load();
    const step = state.currentStep;

    // Log error
    await this.state.logError(step, error);

    // Check retry count
    const retryCount = await this.state.incrementRetry(step);

    // Emit retry attempt
    this._emitEvent('retry:attempt', {
      step,
      attempt: retryCount,
      maxRetries: 3,
      error: error.message,
      timestamp: Date.now(),
    });

    if (retryCount >= 3) {
      console.log('[Runner] Max retries exceeded, rolling back...');

      // Emit rollback start
      this._emitEvent('rollback:start', {
        step,
        reason: 'max-retries-exceeded',
        timestamp: Date.now(),
      });

      // Execute rollback with progress events
      const rollbackSteps = ['tasks', 'approval-gate', 'state'];
      for (let i = 0; i < rollbackSteps.length; i++) {
        const rollbackStep = rollbackSteps[i];

        // Emit rollback progress
        this._emitEvent('rollback:progress', {
          step,
          rollbackStep,
          progress: Math.round(((i + 1) / rollbackSteps.length) * 100),
          timestamp: Date.now(),
        });
      }

      await this.rollback.execute(state);

      // Emit rollback complete
      this._emitEvent('rollback:complete', {
        step,
        success: true,
        timestamp: Date.now(),
      });

      await this.state.update({ currentStep: 'failed' });
    }
  }

  /**
   * Generate final summary
   */
  generateSummary(state) {
    const reqCount = state.analysis?.requirements?.length || 0;
    const discoveryCount = Object.keys(state.idMapping).filter((k) =>
      k.startsWith('discovery-'),
    ).length;
    const implementCount = Object.keys(state.idMapping).filter(
      (k) =>
        k.startsWith('implement-') &&
        !k.includes('-red') &&
        !k.includes('-green') &&
        !k.includes('-refactor') &&
        !k.includes('-syntax'),
    ).length;
    const tddCount = Object.keys(state.idMapping).filter(
      (k) =>
        k.includes('-red') ||
        k.includes('-green') ||
        k.includes('-refactor') ||
        k.includes('-syntax'),
    ).length;

    return {
      sessionId: this.sessionId,
      success: true,
      featureName: state.analysis?.featureName,
      approvalGate: state.idMapping.approvalGate,
      stats: {
        requirements: reqCount,
        discoveryTasks: discoveryCount,
        implementationTasks: implementCount,
        tddSubTasks: tddCount,
        totalTasks: Object.keys(state.idMapping).length,
      },
      executionTime: Date.now() - new Date(state.startedAt).getTime(),
    };
  }

  // Helper methods
  async readPrdFile(prdPath) {
    return await fs.readFile(prdPath, 'utf8');
  }

  parsePRD(content) {
    // Parse PRD and extract ALL requirements (FRs, NFRs, User Stories, Setup, Docs, QA)
    const lines = content.split('\n');
    const requirements = [];
    let reqCounter = 1;

    lines.forEach((line) => {
      // Match requirement headers: FR-*, NFR-*, US-*, Setup-*, Doc-*, QA-*
      // Relaxed to accept: h3/h4, FR1/FR-1/FR-001 formats
      const match = line.match(
        /^#{3,4}\s+(FR-?\d+[a-z]?|NFR-?\d+[a-z]?|US-?\d+|Setup-?\d+|Doc-?\d+|QA-?\d+):?\s*(.+)$/i,
      );
      if (match) {
        const reqId = `REQ-${String(reqCounter).padStart(3, '0')}`;
        reqCounter++;
        requirements.push({
          id: reqId,
          originalId: match[1].toUpperCase(), // Store original ID for reference
          title: match[2].trim(),
          acceptanceCriteria: [],
        });
      }
    });

    return { title: 'Parsed PRD', requirements };
  }

  /**
   * Normalize task title - remove numbered prefixes and reformat
   * Ensures compliance with PRD-exact title requirement
   * Note: Preserves (Part X/Y) suffixes for TDD task uniqueness
   */
  normalizeTitle(title) {
    if (!title) return title;

    // Remove numbered prefixes like "1.0 ", "2.0 ", "10.1 ", etc.
    let normalized = title.replace(/^\d+\.\d+\s+/, '').trim();

    // Remove "Developer - " pattern (case insensitive)
    normalized = normalized.replace(/\s*Developer\s+-\s*/gi, ' ').trim();

    // Remove "Discovery & Planning" generic text
    normalized = normalized
      .replace(/Discovery\s*&\s*Planning/i, 'Discovery')
      .trim();

    // Note: We intentionally DO NOT remove (Part X/Y) suffixes here
    // They are needed for TDD task title uniqueness in multi-part requirements
    // The suffix removal should only happen for consolidated display titles,
    // not for task generation where uniqueness matters

    // Remove "Discovery & Planning [Developer - ...]" suffix pattern
    normalized = normalized.replace(/\s*\[Developer\s*-[^\]]*\]/i, '').trim();

    // Clean up extra whitespace
    normalized = normalized.replace(/\s+/g, ' ').trim();

    return normalized;
  }
}

export default TaskBreakdownRunner;
