/**
 * RollbackManager - Rollback created resources on failure
 * 
 * Closes all created beads in reverse order
 */

import { execSync } from 'child_process';

export class RollbackManager {
  /**
   * Execute rollback - close all created beads
   * @param {Object} state - Current state with idMapping
   */
  async execute(state) {
    console.log('[Rollback] Starting rollback...');
    
    if (!state.idMapping) {
      throw new Error('No idMapping found in state');
    }
    
    const createdIds = Object.values(state.idMapping);
    
    // Reverse order (delete in reverse of creation)
    for (let i = createdIds.length - 1; i >= 0; i--) {
      const id = createdIds[i];
      try {
        await this.closeBead(id);
        console.log(`  ✓ Closed: ${id}`);
      } catch (error) {
        // Ignore errors during rollback (continue closing others)
        console.log(`  ⚠ Failed to close ${id}: ${error.message}`);
      }
    }
    
    console.log('[Rollback] Complete');
  }

  /**
   * Close a single bead
   * @param {string} id - Bead ID to close
   */
  async closeBead(id) {
    const cmd = `br close ${id} --reason "Rollback due to failure"`;
    this.executeCommand(cmd);
  }

  /**
   * Execute command (for testing/mocking purposes)
   * @param {string} cmd - Command to execute
   * @returns {string} Command output
   */
  executeCommand(cmd) {
    return execSync(cmd, { encoding: 'utf8', timeout: 10000 });
  }
}

export default RollbackManager;