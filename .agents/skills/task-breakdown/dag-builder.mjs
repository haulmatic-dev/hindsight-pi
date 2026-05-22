/**
 * DAG Builder for Type-Aware Task Dependencies
 * 
 * Constructs a proper Directed Acyclic Graph (DAG) for task execution,
 * respecting semantic dependencies between different requirement types.
 */

import { TaskPattern, DependencyType } from './analyzer.mjs';

/**
 * DAG Node representing a task in the execution graph
 */
export class DAGNode {
  constructor(id, data = {}) {
    this.id = id;
    this.data = {
      type: data.type || 'unknown',
      taskPattern: data.taskPattern || TaskPattern.TDD_CHAIN,
      needsWorktree: data.needsWorktree !== false,
      originalId: data.originalId || id,
      title: data.title || id,
      ...data
    };
    this.dependencies = new Set(); // IDs of tasks this node depends on
    this.dependents = new Set();   // IDs of tasks that depend on this node
    this.depth = 0;                // Computed depth in graph (0 = root)
    this.phase = null;             // Assigned execution phase
  }

  addDependency(nodeId) {
    this.dependencies.add(nodeId);
  }

  addDependent(nodeId) {
    this.dependents.add(nodeId);
  }

  getDegree() {
    return this.dependencies.size;
  }
}

/**
 * Directed Acyclic Graph for task execution
 */
export class TaskDAG {
  constructor() {
    this.nodes = new Map();
    this.edges = [];
  }

  addNode(id, data = {}) {
    if (!this.nodes.has(id)) {
      this.nodes.set(id, new DAGNode(id, data));
    }
    return this.nodes.get(id);
  }

  addEdge(fromId, toId, type = DependencyType.BLOCKS) {
    const fromNode = this.nodes.get(fromId);
    const toNode = this.nodes.get(toId);
    
    if (!fromNode || !toNode) {
      throw new Error(`Cannot add edge: node not found (${fromId} -> ${toId})`);
    }

    // Check for cycles
    if (this.wouldCreateCycle(fromId, toId)) {
      throw new Error(`Cycle detected: adding edge ${fromId} -> ${toId} would create a cycle`);
    }

    fromNode.addDependent(toId);
    toNode.addDependency(fromId);
    
    this.edges.push({ from: fromId, to: toId, type });
  }

  wouldCreateCycle(fromId, toId) {
    // Quick check: if toId already depends on fromId (transitively), adding this edge creates a cycle
    const visited = new Set();
    const stack = [toId];
    
    while (stack.length > 0) {
      const current = stack.pop();
      if (current === fromId) return true;
      if (visited.has(current)) continue;
      visited.add(current);
      
      const node = this.nodes.get(current);
      if (node) {
        for (const dep of node.dependencies) {
          stack.push(dep);
        }
      }
    }
    
    return false;
  }

  getNode(id) {
    return this.nodes.get(id);
  }

  getRoots() {
    return Array.from(this.nodes.values()).filter(node => node.getDegree() === 0);
  }

  getLeaves() {
    return Array.from(this.nodes.values()).filter(node => node.dependents.size === 0);
  }

  /**
   * Calculate depth (topological level) for each node
   */
  calculateDepths() {
    const visited = new Set();
    const memo = new Map();

    const calculate = (nodeId) => {
      if (memo.has(nodeId)) return memo.get(nodeId);
      if (visited.has(nodeId)) {
        throw new Error(`Cycle detected involving node ${nodeId}`);
      }
      
      visited.add(nodeId);
      const node = this.nodes.get(nodeId);
      
      let maxDepth = 0;
      for (const depId of node.dependencies) {
        maxDepth = Math.max(maxDepth, calculate(depId) + 1);
      }
      
      memo.set(nodeId, maxDepth);
      node.depth = maxDepth;
      return maxDepth;
    };

    for (const [id] of this.nodes) {
      calculate(id);
    }
  }

  /**
   * Group nodes by execution phase based on patterns and dependencies
   */
  calculatePhases() {
    this.calculateDepths();
    
    const phases = [];
    const assigned = new Set();
    
    // Phase 1: Foundation (FRs and Configs with no dependencies except approval)
    const foundation = Array.from(this.nodes.values()).filter(node => {
      if (node.data.taskPattern === TaskPattern.VERIFICATION_GATE) return false;
      const realDeps = Array.from(node.dependencies).filter(id => id !== 'APPROVAL' && id !== 'approvalGate');
      return realDeps.length === 0;
    });
    
    if (foundation.length > 0) {
      phases.push({
        name: 'Foundation',
        type: 'PARALLEL_WORKTREES',
        description: 'Independent FRs and configuration tasks',
        nodes: foundation.map(n => n.id),
        canParallelize: true,
      });
      foundation.forEach(n => {
        assigned.add(n.id);
        n.phase = 'Foundation';
      });
    }
    
    // Phase 2: Integration (User Stories and dependent FRs)
    const integration = Array.from(this.nodes.values()).filter(node => {
      if (assigned.has(node.id)) return false;
      if (node.data.taskPattern === TaskPattern.VERIFICATION_GATE) return false;
      // All dependencies must be assigned to earlier phases
      return Array.from(node.dependencies).every(depId => assigned.has(depId) || depId === 'APPROVAL' || depId === 'approvalGate');
    });
    
    if (integration.length > 0) {
      phases.push({
        name: 'Integration',
        type: 'SEQUENTIAL_WORKTREES',
        description: 'User Stories aggregating FRs and dependent tasks',
        nodes: integration.map(n => n.id),
        canParallelize: true,
        dependsOn: 'Foundation',
      });
      integration.forEach(n => {
        assigned.add(n.id);
        n.phase = 'Integration';
      });
    }
    
    // Phase 3: Verification Gates (NFRs that verify quality)
    const gates = Array.from(this.nodes.values()).filter(node => {
      if (assigned.has(node.id)) return false;
      return node.data.taskPattern === TaskPattern.VERIFICATION_GATE;
    });
    
    if (gates.length > 0) {
      phases.push({
        name: 'Quality Verification',
        type: 'VERIFICATION_GATES',
        description: 'Quality gates running in main worktree (no isolation needed)',
        nodes: gates.map(n => n.id),
        canParallelize: true, // Gates can run in parallel with each other
        dependsOn: 'Integration',
        needsWorktree: false,
      });
      gates.forEach(n => {
        assigned.add(n.id);
        n.phase = 'Quality Verification';
      });
    }
    
    // Phase 4: Final Gate
    const finalGates = Array.from(this.nodes.values()).filter(node => !assigned.has(node.id));
    if (finalGates.length > 0) {
      phases.push({
        name: 'Final Verification',
        type: 'FINAL_GATE',
        description: 'Final comprehensive checks before merge',
        nodes: finalGates.map(n => n.id),
        canParallelize: false,
        dependsOn: 'Quality Verification',
        needsWorktree: false,
      });
      finalGates.forEach(n => {
        assigned.add(n.id);
        n.phase = 'Final Verification';
      });
    }
    
    return phases;
  }

  /**
   * Get execution plan with worktree assignments
   */
  getExecutionPlan() {
    const phases = this.calculatePhases();
    
    return {
      phases,
      totalNodes: this.nodes.size,
      totalEdges: this.edges.length,
      worktreeCount: Array.from(this.nodes.values()).filter(n => n.data.needsWorktree).length,
      gateCount: Array.from(this.nodes.values()).filter(n => !n.data.needsWorktree).length,
      criticalPath: this.findCriticalPath(),
    };
  }

  /**
   * Find the critical path (longest path from root to leaf)
   */
  findCriticalPath() {
    const leaves = this.getLeaves();
    let longestPath = [];
    
    for (const leaf of leaves) {
      const path = this.tracePath(leaf.id);
      if (path.length > longestPath.length) {
        longestPath = path;
      }
    }
    
    return longestPath;
  }

  tracePath(nodeId, visited = new Set()) {
    if (visited.has(nodeId)) return [];
    visited.add(nodeId);
    
    const node = this.nodes.get(nodeId);
    if (!node || node.dependencies.size === 0) {
      return [nodeId];
    }
    
    // Find longest dependency path
    let longestSubPath = [];
    for (const depId of node.dependencies) {
      const subPath = this.tracePath(depId, new Set(visited));
      if (subPath.length > longestSubPath.length) {
        longestSubPath = subPath;
      }
    }
    
    return [...longestSubPath, nodeId];
  }

  /**
   * Export to Graphviz DOT format for visualization
   */
  toDOT() {
    const lines = ['digraph TaskGraph {'];
    lines.push('  rankdir=TB;');
    lines.push('  node [shape=box, style="rounded,filled"];');
    
    // Define nodes with colors by type
    for (const [id, node] of this.nodes) {
      let color = 'lightblue';
      if (node.data.taskPattern === TaskPattern.VERIFICATION_GATE) color = 'lightgreen';
      if (node.data.taskPattern === TaskPattern.AGGREGATION_TASK) color = 'lightyellow';
      if (node.data.taskPattern === TaskPattern.CONFIGURATION_TASK) color = 'lightcoral';
      
      const label = node.data.title || id;
      lines.push(`  "${id}" [label="${label}", fillcolor=${color}];`);
    }
    
    // Define edges
    for (const edge of this.edges) {
      const style = edge.type === DependencyType.VERIFIES ? 'dashed' : 'solid';
      lines.push(`  "${edge.from}" -> "${edge.to}" [style=${style}];`);
    }
    
    lines.push('}');
    return lines.join('\n');
  }

  /**
   * Export to Mermaid format for markdown embedding
   */
  toMermaid() {
    const lines = ['graph TD;'];
    
    for (const [id, node] of this.nodes) {
      const safeId = id.replace(/[^a-zA-Z0-9]/g, '_');
      const label = (node.data.title || id).substring(0, 30);
      
      let style = '';
      if (node.data.taskPattern === TaskPattern.VERIFICATION_GATE) {
        style = ` style ${safeId} fill:#90EE90`;
      } else if (node.data.taskPattern === TaskPattern.AGGREGATION_TASK) {
        style = ` style ${safeId} fill:#FFE4B5`;
      }
      
      lines.push(`  ${safeId}["${label}"]${style};`);
    }
    
    for (const edge of this.edges) {
      const fromSafe = edge.from.replace(/[^a-zA-Z0-9]/g, '_');
      const toSafe = edge.to.replace(/[^a-zA-Z0-9]/g, '_');
      const style = edge.type === DependencyType.VERIFIES ? '-.->' : '-->';
      lines.push(`  ${fromSafe} ${style} ${toSafe};`);
    }
    
    return lines.join('\n');
  }
}

/**
 * Build DAG from analyzed requirements
 */
export function buildDAGFromRequirements(requirements, approvalGateId) {
  const dag = new TaskDAG();
  
  // Add approval gate as root
  dag.addNode('approvalGate', {
    type: 'gate',
    taskPattern: 'approval',
    needsWorktree: false,
    title: 'Approval Gate',
  });
  
  // Add all requirements as nodes
  for (const req of requirements) {
    dag.addNode(req.id, {
      type: req.type,
      taskPattern: req.taskPattern,
      needsWorktree: req.needsWorktree,
      originalId: req.originalId,
      title: req.title,
      dependsOn: req.dependsOn || [],
    });
  }
  
  // Add edges based on dependencies
  for (const req of requirements) {
    if (req.dependsOn && req.dependsOn.length > 0) {
      for (const depId of req.dependsOn) {
        // Map dependency ID to node ID
        const depReq = requirements.find(r => r.originalId === depId || r.id === depId);
        if (depReq) {
          dag.addEdge(depReq.id, req.id, req.dependencyType || DependencyType.BLOCKS);
        } else if (depId === 'approvalGate' || depId === 'APPROVAL') {
          dag.addEdge('approvalGate', req.id, DependencyType.BLOCKS);
        }
      }
    } else {
      // No explicit dependencies = depends on approval gate
      dag.addEdge('approvalGate', req.id, DependencyType.BLOCKS);
    }
  }
  
  return dag;
}

/**
 * Validate DAG for common issues
 */
export function validateDAG(dag) {
  const issues = [];
  
  // Check for isolated nodes
  for (const [id, node] of dag.nodes) {
    if (node.dependencies.size === 0 && node.dependents.size === 0 && id !== 'approvalGate') {
      issues.push({ type: 'warning', message: `Node ${id} is isolated (no dependencies)` });
    }
  }
  
  // Check for verification gates depending on other gates (inefficient)
  const gates = Array.from(dag.nodes.values()).filter(n => 
    n.data.taskPattern === TaskPattern.VERIFICATION_GATE
  );
  
  for (const gate of gates) {
    const dependsOnGates = Array.from(gate.dependencies).filter(depId => {
      const dep = dag.getNode(depId);
      return dep && dep.data.taskPattern === TaskPattern.VERIFICATION_GATE;
    });
    
    if (dependsOnGates.length > 0) {
      issues.push({ 
        type: 'info', 
        message: `Gate ${gate.id} depends on other gates: ${dependsOnGates.join(', ')}` 
      });
    }
  }
  
  // Check for User Stories not depending on any FRs
  const userStories = Array.from(dag.nodes.values()).filter(n => 
    n.data.taskPattern === TaskPattern.AGGREGATION_TASK
  );
  
  for (const us of userStories) {
    const dependsOnFRs = Array.from(us.dependencies).filter(depId => {
      const dep = dag.getNode(depId);
      return dep && dep.data.type === 'FR';
    });
    
    if (dependsOnFRs.length === 0) {
      issues.push({ 
        type: 'warning', 
        message: `User Story ${us.id} doesn't depend on any FRs` 
      });
    }
  }
  
  return issues;
}

export default { TaskDAG, DAGNode, buildDAGFromRequirements, validateDAG };
