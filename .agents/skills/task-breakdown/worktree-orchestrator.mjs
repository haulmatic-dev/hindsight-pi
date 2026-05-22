/**
 * Worktree Orchestrator
 *
 * Manages Git worktree allocation for parallel task execution.
 * Ensures optimal resource usage by only creating worktrees when necessary.
 */

import { TaskPattern } from './analyzer.mjs';

/**
 * Worktree allocation strategy
 */
export class WorktreeOrchestrator {
  constructor(options = {}) {
    this.maxWorktrees = options.maxWorktrees || 10;
    this.worktreePrefix = options.worktreePrefix || '../wt-';
    this.baseBranch = options.baseBranch || 'main';
    this.activeWorktrees = new Map();
    this.completedWorktrees = new Set();
  }

  /**
   * Allocate worktrees based on execution phases
   */
  allocateWorktrees(phases) {
    const allocation = {
      phases: [],
      totalWorktrees: 0,
      maxConcurrent: 0,
      reuseStrategy: [],
    };

    for (const phase of phases) {
      const phaseAllocation = this.allocatePhaseWorktrees(phase);
      allocation.phases.push(phaseAllocation);
      allocation.totalWorktrees += phaseAllocation.worktrees.length;
      allocation.maxConcurrent = Math.max(
        allocation.maxConcurrent,
        phaseAllocation.concurrentCount,
      );
    }

    // Calculate reuse opportunities
    allocation.reuseStrategy = this.calculateReuseStrategy(allocation.phases);

    return allocation;
  }

  /**
   * Allocate worktrees for a single phase
   */
  allocatePhaseWorktrees(phase) {
    const worktrees = [];
    let concurrentCount = 0;

    if (phase.type === 'VERIFICATION_GATES' || phase.type === 'FINAL_GATE') {
      // Gates don't need worktrees - run in main worktree
      return {
        phase: phase.name,
        type: phase.type,
        worktrees: [],
        concurrentCount: 0,
        notes: 'Verification gates run in main worktree (no isolation needed)',
      };
    }

    for (const nodeId of phase.nodes) {
      // Determine worktree configuration
      const worktree = this.createWorktreeConfig(nodeId, phase);
      worktrees.push(worktree);
      concurrentCount++;
    }

    return {
      phase: phase.name,
      type: phase.type,
      worktrees,
      concurrentCount,
      canParallelize: phase.canParallelize !== false,
    };
  }

  /**
   * Create worktree configuration for a node
   */
  createWorktreeConfig(nodeId, phase) {
    const sanitizedId = nodeId.replace(/[^a-zA-Z0-9]/g, '-').toLowerCase();
    const worktreeName = `${this.worktreePrefix}${sanitizedId}`;
    const branchName = `feature/${sanitizedId}`;

    return {
      nodeId,
      worktreeName,
      branchName,
      baseBranch: this.getBaseBranchForPhase(phase),
      purpose: this.getWorktreePurpose(nodeId, phase),
      estimatedSize: '650MB', // Typical size with node_modules
      lifecycle: {
        created: null,
        activated: null,
        completed: null,
        removed: null,
      },
    };
  }

  /**
   * Determine base branch for a phase
   */
  getBaseBranchForPhase(phase) {
    switch (phase.name) {
      case 'Foundation':
        return this.baseBranch;
      case 'Integration':
        return 'feature/foundation-merged'; // After Phase 1 merges
      case 'Quality Verification':
        return 'feature/integration-complete'; // After Phase 2 merges
      default:
        return this.baseBranch;
    }
  }

  /**
   * Get human-readable purpose for worktree
   */
  getWorktreePurpose(nodeId, phase) {
    const purposes = {
      Foundation: 'Independent feature implementation',
      Integration: 'Feature aggregation and integration',
      'Quality Verification': 'Not applicable - gates run in main worktree',
    };
    return purposes[phase.name] || 'Task implementation';
  }

  /**
   * Calculate worktree reuse strategy
   */
  calculateReuseStrategy(phases) {
    const strategy = [];

    // Sequential phases can reuse worktrees after previous phase completes
    for (let i = 1; i < phases.length; i++) {
      const prevPhase = phases[i - 1];
      const currPhase = phases[i];

      if (prevPhase.worktrees.length > 0 && currPhase.worktrees.length > 0) {
        strategy.push({
          action: 'REUSE',
          from: prevPhase.phase,
          to: currPhase.phase,
          note: `Reuse ${Math.min(prevPhase.worktrees.length, currPhase.worktrees.length)} worktrees after cleanup`,
        });
      }
    }

    return strategy;
  }

  /**
   * Generate Git commands for worktree setup
   */
  generateSetupCommands(allocation) {
    const commands = [];

    commands.push('#!/bin/bash');
    commands.push('# Auto-generated worktree setup script');
    commands.push('');
    commands.push(`cd "$(dirname "$0")"`);
    commands.push('');

    for (const phase of allocation.phases) {
      commands.push(`# ${phase.phase} - ${phase.type}`);

      if (phase.worktrees.length === 0) {
        commands.push('# No worktrees needed (verification gates)');
        commands.push('');
        continue;
      }

      for (const wt of phase.worktrees) {
        commands.push(`# Create worktree for ${wt.nodeId}`);
        commands.push(`git worktree add -b ${wt.branchName} ${wt.worktreeName} ${wt.baseBranch}`);
        commands.push(`cd ${wt.worktreeName}`);
        commands.push('# Install dependencies if needed');
        commands.push('[ -f package.json ] && npm install');
        commands.push('cd -');
        commands.push('');
      }
    }

    return commands.join('\n');
  }

  /**
   * Generate Git commands for worktree cleanup
   */
  generateCleanupCommands(allocation) {
    const commands = [];

    commands.push('#!/bin/bash');
    commands.push('# Auto-generated worktree cleanup script');
    commands.push('');

    for (const phase of allocation.phases) {
      for (const wt of phase.worktrees) {
        commands.push(`# Remove worktree for ${wt.nodeId}`);
        commands.push(`git worktree remove ${wt.worktreeName} --force || true`);
        commands.push(`git branch -D ${wt.branchName} || true`);
        commands.push('');
      }
    }

    commands.push('# Prune any remaining worktree metadata');
    commands.push('git worktree prune');

    return commands.join('\n');
  }

  /**
   * Generate Hydra worker allocation plan
   */
  generateHydraPlan(allocation) {
    const plan = {
      version: '2.0',
      description: 'Type-aware worktree allocation plan',
      phases: [],
      resourceEstimate: {
        totalWorktrees: allocation.totalWorktrees,
        maxConcurrentWorktrees: allocation.maxConcurrent,
        estimatedDiskSpace: `${allocation.totalWorktrees * 650}MB`,
        estimatedMemory: `${allocation.maxConcurrent * 2}GB`,
      },
    };

    for (const phase of allocation.phases) {
      const phasePlan = {
        name: phase.phase,
        type: phase.type,
        executionMode: phase.canParallelize ? 'PARALLEL' : 'SEQUENTIAL',
        tasks: [],
      };

      if (phase.type === 'VERIFICATION_GATES' || phase.type === 'FINAL_GATE') {
        // Gates run in main worktree
        phasePlan.executionMode = 'SEQUENTIAL'; // Gates should run sequentially
        phasePlan.worktree = null; // Main worktree
        phasePlan.tasks = phase.nodes.map((nodeId) => ({
          nodeId,
          worktree: null,
          workerMode: 'verification',
          notes: 'Runs in main worktree - no isolation needed',
        }));
      } else {
        // Regular tasks get worktrees
        for (const wt of phase.worktrees) {
          phasePlan.tasks.push({
            nodeId: wt.nodeId,
            worktree: wt.worktreeName,
            branch: wt.branchName,
            workerMode: 'worktree',
            baseBranch: wt.baseBranch,
          });
        }
      }

      plan.phases.push(phasePlan);
    }

    return plan;
  }

  /**
   * Estimate resource requirements
   */
  estimateResources(allocation) {
    const worktrees = allocation.totalWorktrees;
    const concurrent = allocation.maxConcurrent;

    return {
      disk: {
        perWorktree: '650MB',
        total: `${worktrees * 650}MB`,
        concurrent: `${concurrent * 650}MB`,
      },
      memory: {
        perWorktree: '2GB',
        total: `${worktrees * 2}GB`,
        concurrent: `${concurrent * 2}GB`,
      },
      cpu: {
        recommended: concurrent, // One core per concurrent worktree
        note: 'Higher is better for parallel builds',
      },
      network: {
        note: 'Download dependencies for each worktree',
        estimatedPerWorktree: '100-500MB',
      },
    };
  }

  /**
   * Compare with old model (pre-type-aware)
   */
  generateComparison(oldWorktreeCount, newAllocation) {
    const newCount = newAllocation.totalWorktrees;
    const reduction = oldWorktreeCount - newCount;
    const percentage = ((reduction / oldWorktreeCount) * 100).toFixed(1);

    return {
      oldModel: {
        worktrees: oldWorktreeCount,
        diskSpace: `${oldWorktreeCount * 650}MB`,
        approach: 'One worktree per requirement',
      },
      newModel: {
        worktrees: newCount,
        diskSpace: `${newCount * 650}MB`,
        approach: 'Worktrees only for implementation tasks',
        gatesInMainWorktree: true,
      },
      savings: {
        worktrees: reduction,
        diskSpace: `${reduction * 650}MB`,
        percentage: `${percentage}%`,
      },
    };
  }
}

/**
 * Create orchestration plan from DAG
 */
export function createOrchestrationPlan(dag, options = {}) {
  const phases = dag.calculatePhases();
  const orchestrator = new WorktreeOrchestrator(options);

  const allocation = orchestrator.allocateWorktrees(phases);
  const executionPlan = orchestrator.generateHydraPlan(allocation);
  const resources = orchestrator.estimateResources(allocation);

  return {
    allocation,
    executionPlan,
    resources,
    setupScript: orchestrator.generateSetupCommands(allocation),
    cleanupScript: orchestrator.generateCleanupCommands(allocation),
  };
}

/**
 * Validate worktree allocation against constraints
 */
export function validateAllocation(allocation, constraints = {}) {
  const maxWorktrees = constraints.maxWorktrees || 10;
  const maxDiskGB = constraints.maxDiskGB || 10;
  const issues = [];

  if (allocation.maxConcurrent > maxWorktrees) {
    issues.push({
      severity: 'error',
      message: `Max concurrent worktrees (${allocation.maxConcurrent}) exceeds limit (${maxWorktrees})`,
      suggestion: 'Increase maxWorktrees or split into more phases',
    });
  }

  const diskGB = (allocation.totalWorktrees * 650) / 1024;
  if (diskGB > maxDiskGB) {
    issues.push({
      severity: 'warning',
      message: `Estimated disk usage (${diskGB.toFixed(1)}GB) exceeds limit (${maxDiskGB}GB)`,
      suggestion: 'Enable worktree reuse or increase disk limit',
    });
  }

  return {
    valid: issues.filter((i) => i.severity === 'error').length === 0,
    issues,
  };
}

export default { WorktreeOrchestrator, createOrchestrationPlan, validateAllocation };
