/**
 * Deterministic PRD Analyzer with Semantic Classification
 *
 * Pure code-based PRD analysis without AI agents.
 * Parses PRD markdown and extracts structured requirements with type-aware classification.
 */

/**
 * Task patterns for different requirement types
 */
export const TaskPattern = {
  TDD_CHAIN: 'tdd_chain',               // FRs: RED→GREEN→REFACTOR→SYNTAX
  VERIFICATION_GATE: 'verification_gate', // NFRs: coverage, quality, perf (no worktree)
  AGGREGATION_TASK: 'aggregation_task',   // USs: integrate FRs
  CONFIGURATION_TASK: 'configuration_task', // Config: module system, setup
};

/**
 * Task routes proven by the fixture suite (task-breakdown/test-pattern-routing.mjs).
 * Route = which execution pattern the requirement content signals.
 */
export const TaskRoute = {
  ANALYSIS: 'analysis',
  TEST_AUTHORING: 'test_authoring',
  REFACTOR: 'refactor',
  SPIKE: 'spike',
  DELETION: 'deletion',
  AUDIT: 'audit',
  LEGACY_NO_SCOPE: 'legacy_no_scope',
  NORMAL_CODE: 'normal_code',
};

/**
 * Route a standalone requirement document (fixture-style markdown) to its
 * task pattern. Detection precedence (first match wins):
 *  1. analysis / spike / deletion / refactor / test / audit via `**Type:**`
 *     markers or `### Spike-###` headers
 *  2. code work: `**Type:** fr` with `**Target Files:**` -> normal_code,
 *     without Target Files -> legacy_no_scope
 *
 * Reuses the same markdown conventions the analyzer's target-file parsing
 * relies on (backticked paths after "Target Files:"), not a second parser.
 * @param {string} content - Raw markdown document
 * @returns {{route: string, reason: string}} Route assignment with evidence
 */
export function routeTaskPattern(content) {
  if (typeof content !== 'string' || content.trim().length === 0) {
    return { route: TaskRoute.LEGACY_NO_SCOPE, reason: 'empty content: no scope or type markers present' };
  }

  const typeMatches = [...content.matchAll(/\*\*Type:\*\*\s*([a-z_-]+)/gi)].map((m) => m[1].toLowerCase());
  const types = new Set(typeMatches);

  // Header-level signals (### Spike-001, "# Analysis: ...")
  const hasSpikeHeader = /^#{2,4}\s+Spike-[A-Z0-9]/im.test(content);
  const hasAnalysisTitle = /^#\s+Analysis:/im.test(content);

  if (types.has('analysis') || hasAnalysisTitle) {
    return { route: TaskRoute.ANALYSIS, reason: 'analysis-type requirement signals investigation/documentation work' };
  }
  if (types.has('spike') || hasSpikeHeader) {
    return { route: TaskRoute.SPIKE, reason: 'spike-type requirement signals time-boxed evaluation' };
  }
  if (types.has('deletion')) {
    return { route: TaskRoute.DELETION, reason: 'deletion-type requirement signals removal-only work' };
  }
  if (types.has('refactor')) {
    return { route: TaskRoute.REFACTOR, reason: 'refactor-type requirement signals behavior-preserving cleanup' };
  }
  if (types.has('test')) {
    return { route: TaskRoute.TEST_AUTHORING, reason: 'test-type requirement signals test-first authoring' };
  }
  if (types.has('audit')) {
    return { route: TaskRoute.AUDIT, reason: 'audit-type requirement signals review-and-report (no fixes)' };
  }

  // Code work: does any requirement carry explicit Target Files scope?
  // Backticked paths after "**Target Files:**" are the analyzer's own
  // target-file convention — no separate parsing scheme.
  const hasTargetFiles = /\*\*Target Files:\*\*\s*`?[^\n]+/i.test(content);
  if (hasTargetFiles) {
    return { route: TaskRoute.NORMAL_CODE, reason: 'code requirement with explicit Target Files scope' };
  }
  return { route: TaskRoute.LEGACY_NO_SCOPE, reason: 'code requirement without Target Files — legacy no-scope shape' };
}

/**
 * Dependency types for semantic relationships
 */
export const DependencyType = {
  BLOCKS: 'blocks',               // Traditional dependency
  VERIFIES: 'verifies',           // NFR verifies FR completion
  AGGREGATES: 'aggregates',       // US aggregates multiple FRs
  PROVIDES_BASE_FOR: 'provides_base_for', // FR provides base for dependent FRs
  CONFIGURES: 'configures',       // Config task enables others
};

/**
 * Analyze PRD content deterministically using regex and parsing
 * @param {string} prdContent - Raw PRD markdown content
 * @returns {Object} Structured analysis with semantic classification
 */
export function analyzePRDDeterministic(prdContent) {
  const lines = prdContent.split('\n');
  const requirements = [];
  let featureName = 'Untitled Feature';
  let reqCounter = 1;
  let currentReq = null;
  let inAcceptanceCriteria = false;
  let inDescription = true;
  let inVerifiesSection = false;
  let inAggregatesSection = false;

  // Extract feature name from title (first H1)
  const titleMatch = prdContent.match(/^#\s+(.+)$/m);
  if (titleMatch) {
    featureName = titleMatch[1].trim();
  }

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const trimmedLine = line.trim();

    // Match requirement headers: ### FR-*, ### NFR-*, ### US-*, etc.
    // NFR IDs may be numeric (NFR-003) or alphanumeric (NFR-SYS)
    const reqMatch = trimmedLine.match(
      /^#{3,4}\s+(FR-?\d+[a-z]?|NFR-[A-Z0-9]+|US-?\d+|US-INT-\d+|SETUP-?\d+|Doc-?\d+|QA-?\d+):?\s*(.+)$/i,
    );

    if (reqMatch) {
      // Save previous requirement if exists
      if (currentReq) {
        requirements.push(currentReq);
      }

      const reqId = `REQ-${String(reqCounter).padStart(3, '0')}`;
      reqCounter++;

      // Determine type from prefix
      let type = 'FR';
      if (reqMatch[1].toUpperCase().startsWith('NFR')) type = 'NFR';
      else if (reqMatch[1].toUpperCase().startsWith('US-INT')) type = 'User Story Integration';
      else if (reqMatch[1].toUpperCase().startsWith('US')) type = 'User Story';
      else if (reqMatch[1].toUpperCase().startsWith('SETUP')) type = 'Setup';
      else if (reqMatch[1].toUpperCase().startsWith('DOC')) type = 'Docs';
      else if (reqMatch[1].toUpperCase().startsWith('QA')) type = 'QA';

      currentReq = {
        id: reqId,
        originalId: reqMatch[1].toUpperCase(),
        title: reqMatch[2].trim(),
        description: '',
        acceptanceCriteria: [],
        estimatedHours: estimateHoursFromDescription(''),
        type: type,
        priority: null,            // From **Priority:** section (P0/P1/P2)
        files: [],
        functions: [],
        dataModels: [],
        integrationPoints: [],
        configuration: [],
        // NEW: Semantic classification fields
        explicitVerifies: [],      // From **Verifies:** section
        explicitAggregates: [],    // From **Aggregates:** section
        explicitDependsOn: [],     // From **Depends On:** section
      };

      inAcceptanceCriteria = false;
      inDescription = true;
      inVerifiesSection = false;
      inAggregatesSection = false;
      continue;
    }

    // If we have a current requirement, accumulate data
    if (currentReq) {
      // Check for semantic sections
      if (trimmedLine.match(/^\*{0,2}priority:?\*{0,2}/i)) {
        const priorityMatch = trimmedLine.match(/^\*{0,2}priority:?\*{0,2}\s*(p[0-3])/i);
        if (priorityMatch && currentReq) {
          currentReq.priority = priorityMatch[1].toUpperCase();
        }
        inVerifiesSection = false;
        inAggregatesSection = false;
        inAcceptanceCriteria = false;
        inDescription = false;
        continue;
      }

      if (trimmedLine.match(/^\*{0,2}target files:?\*{0,2}/i)) {
        const filesMatch = trimmedLine.match(/^\*{0,2}target files:?\*{0,2}\s+(.+)/i);
        if (filesMatch && currentReq) {
          const files = filesMatch[1]
            .split(/[,\s]+/)
            .map(f => f.trim().replace(/^`|`$/g, '').replace(/^"|"$/g, ''))
            .filter(f => f && f.length > 0);
          for (const f of files) {
            if (!currentReq.files.includes(f)) {
              currentReq.files.push(f);
            }
          }
        }
        inVerifiesSection = false;
        inAggregatesSection = false;
        inAcceptanceCriteria = false;
        inDescription = false;
        continue;
      }

      if (trimmedLine.match(/^\*{0,2}reads from:?\*{0,2}/i)) {
        const readsMatch = trimmedLine.match(/^\*{0,2}reads from:?\*{0,2}\s+(.+)/i);
        if (readsMatch && currentReq) {
          const files = readsMatch[1]
            .split(/[,\s]+/)
            .map(f => f.trim().replace(/^`|`$/g, '').replace(/^"|"$/g, ''))
            .filter(f => f && f.length > 0);
          for (const f of files) {
            if (!currentReq.files.includes(f)) {
              currentReq.files.push(f);
            }
          }
        }
        inVerifiesSection = false;
        inAggregatesSection = false;
        inAcceptanceCriteria = false;
        inDescription = false;
        continue;
      }

      // Parse Test Command and Build Command (stack-agnostic contract fields)
      if (trimmedLine.match(/^\*{0,2}test command:?\*{0,2}/i)) {
        const tcMatch = trimmedLine.match(/^\*{0,2}test command:?\*{0,2}\s+(.+)/i);
        if (tcMatch && currentReq) {
          currentReq.testCommand = tcMatch[1].trim().replace(/^`|`$/g, '');
        }
        inVerifiesSection = false;
        inAggregatesSection = false;
        inAcceptanceCriteria = false;
        inDescription = false;
        continue;
      }

      if (trimmedLine.match(/^\*{0,2}build command:?\*{0,2}/i)) {
        const bcMatch = trimmedLine.match(/^\*{0,2}build command:?\*{0,2}\s+(.+)/i);
        if (bcMatch && currentReq) {
          currentReq.buildCommand = bcMatch[1].trim().replace(/^`|`$/g, '');
        }
        inVerifiesSection = false;
        inAggregatesSection = false;
        inAcceptanceCriteria = false;
        inDescription = false;
        continue;
      }

      if (trimmedLine.match(/^\*{0,2}verifies:?\*{0,2}/i)) {
        inVerifiesSection = true;
        inAggregatesSection = false;
        inAcceptanceCriteria = false;
        inDescription = false;
        continue;
      }
      
      if (trimmedLine.match(/^\*{0,2}aggregates?:?\*{0,2}/i)) {
        inVerifiesSection = false;
        inAggregatesSection = true;
        inAcceptanceCriteria = false;
        inDescription = false;
        continue;
      }
      
      if (trimmedLine.match(/^\*{0,2}depends on:?\*{0,2}/i)) {
        // Parse depends on section
        inVerifiesSection = false;
        inAggregatesSection = false;
        inAcceptanceCriteria = false;
        inDescription = false;
        continue;
      }

      // Check for acceptance criteria section
      if (trimmedLine.match(/^\*{0,2}(####?\s+)?acceptance criteria\*{0,2}/i)) {
        inAcceptanceCriteria = true;
        inVerifiesSection = false;
        inAggregatesSection = false;
        inDescription = false;
        continue;
      }

      // Check for end of sections
      if (trimmedLine.match(/^#{1,4}\s/)) {
        inAcceptanceCriteria = false;
        inVerifiesSection = false;
        inAggregatesSection = false;
        // Only continue description accumulation for requirement headers
        // Non-requirement headers (like ## 5. Technical Considerations) stop description
        const isReqHeader = trimmedLine.match(
          /^#{3,4}\s+(FR-?\d+[a-z]?|NFR-?\d+[a-z]?|US-?\d+|Setup-?\d+|Doc-?\d+|QA-?\d+):?/i,
        );
        inDescription = !!isReqHeader;
      }

      // Parse Verifies section entries
      if (inVerifiesSection && trimmedLine.match(/^-\s+(.+)$/)) {
        const entry = trimmedLine.replace(/^-\s+/, '').trim();
        // Extract requirement ID if present (e.g., "FR-001: Addition" or just "FR-001")
        const idMatch = entry.match(/^(FR-\d+|NFR-\d+|US-\d+)/i);
        if (idMatch) {
          currentReq.explicitVerifies.push(idMatch[1].toUpperCase());
        }
        continue;
      }

      // Parse Aggregates section entries
      if (inAggregatesSection && trimmedLine.match(/^-\s+(.+)$/)) {
        const entry = trimmedLine.replace(/^-\s+/, '').trim();
        const idMatch = entry.match(/^(FR-\d+|NFR-\d+|US-\d+)/i);
        if (idMatch) {
          currentReq.explicitAggregates.push(idMatch[1].toUpperCase());
        }
        continue;
      }

      // Parse acceptance criteria
      if (inAcceptanceCriteria && trimmedLine.match(/^-\s+(.+)$/)) {
        let acText = trimmedLine.replace(/^-\s+/, '').trim();
        // Strip ID prefixes
        acText = acText.replace(
          /^(?:FR|NFR|US|Setup|Doc|QA|AC)\d+(?:\.\d+)?[a-z]?:\s*/i,
          '',
        );
        currentReq.acceptanceCriteria.push(acText);
        continue;
      }

      // Accumulate description
      if (inDescription && trimmedLine) {
        if (currentReq.description) {
          currentReq.description += '\n';
        }
        currentReq.description += trimmedLine;
      }
    }
  }

  // Don't forget the last requirement
  if (currentReq) {
    requirements.push(currentReq);
  }

  // Post-process: extract additional data and classify
  const globalContext = {
    integrationPoints: extractIntegrationPoints(prdContent),
    dataModels: extractDataModels(prdContent),
    configuration: extractConfiguration(prdContent),
    architecture: extractArchitectureSection(prdContent),
  };

  // Enrich each requirement with context and semantic classification
  for (const req of requirements) {
    req.estimatedHours = estimateHoursFromDescription(req.description);
    extractRequirementContextEnhanced(req, prdContent, globalContext);

    // Set default priority if not explicitly provided
    if (!req.priority) {
      if (req.taskPattern === 'configuration_task' || req.type === 'Setup') {
        req.priority = 'P0';
      } else if (req.type === 'NFR' && req.taskPattern === 'verification_gate') {
        req.priority = 'P2';
      } else {
        req.priority = 'P1';
      }
    }

    // NEW: Apply semantic classification
    classifyRequirement(req, requirements);
  }

  // Detect dependencies based on requirement references
  const dependencies = detectDependencies(requirements);
  
  // Detect cross-cutting concerns (FRs that modify same files as other FRs)
  const crossCuttingDeps = detectCrossCuttingConcerns(requirements);
  for (const [reqId, deps] of crossCuttingDeps) {
    const req = requirements.find(r => r.id === reqId);
    if (req) {
      req.explicitDependsOn = [...new Set([...(req.explicitDependsOn || []), ...deps])];
      req.dependsOn = req.explicitDependsOn;
    }
  }
  
  // Build semantic dependency graph
  const semanticGraph = buildSemanticDependencyGraph(requirements, dependencies);

  // Validate requirements have actionable targets
  validateRequirements(requirements);

  // Validate no two FRs share the same target file (prevents merge conflicts)
  const overlapErrors = validateFileOverlap(requirements);
  if (overlapErrors.length > 0) {
    throw new Error(`PRD validation failed: target file overlap detected.\n${overlapErrors.join('\n')}`);
  }

  return {
    featureName,
    requirements,
    dependencies,
    semanticGraph,
    hasNFRs: requirements.some((r) => r.type === 'NFR'),
    hasUserStories: requirements.some((r) => r.type === 'User Story'),
    globalContext,
  };
}

/**
 * Classify requirement with task pattern and dependencies
 * This is the core enhancement for type-aware task generation
 */
function classifyRequirement(req, allRequirements) {
  // Get list of all FR IDs for reference
  const allFRs = allRequirements.filter(r => r.type === 'FR').map(r => r.originalId);
  const allReqsById = new Map(allRequirements.map(r => [r.originalId, r]));

  switch (req.type) {
    case 'FR':
      // Functional requirements get TDD chains
      req.taskPattern = TaskPattern.TDD_CHAIN;
      req.needsWorktree = true;
      req.dependsOn = ['approvalGate'];
      req.dependencyType = DependencyType.BLOCKS;
      
      // Check if this FR provides base for other FRs
      const dependentFRs = allRequirements.filter(r => 
        r.type === 'FR' && 
        r.explicitDependsOn?.includes(req.originalId)
      );
      if (dependentFRs.length > 0) {
        req.providesBaseFor = dependentFRs.map(r => r.originalId);
        req.dependencyType = DependencyType.PROVIDES_BASE_FOR;
      }
      break;

    case 'NFR':
      // Distinguish verification NFRs from configuration NFRs
      if (isVerificationNFR(req)) {
        // Test coverage, code quality, documentation completeness, performance benchmarks
        req.taskPattern = TaskPattern.VERIFICATION_GATE;
        req.needsWorktree = false;
        
        // Use explicit verifies list if provided
        if (req.explicitVerifies.length > 0) {
          req.dependsOn = req.explicitVerifies;
        } else {
          // FIXED: Gates depend on ALL implementation chains, not just FRs
          // This ensures gates verify: FRs + Light USs + Config tasks
          req.dependsOn = allRequirements
            .filter(r => 
              r.type === 'FR' || 
              (r.type === 'User Story' && r.taskPattern === TaskPattern.AGGREGATION_TASK) ||
              r.taskPattern === TaskPattern.CONFIGURATION_TASK
            )
            .map(r => r.originalId);
          
          console.log(`    🔍 Gate ${req.originalId} verifies ${req.dependsOn.length} implementation chains`);
        }
        req.dependencyType = DependencyType.VERIFIES;
      } else if (isConfigurationNFR(req)) {
        // Module system, build config, package setup
        req.taskPattern = TaskPattern.CONFIGURATION_TASK;
        req.needsWorktree = true;
        req.dependsOn = ['approvalGate'];
        req.dependencyType = DependencyType.CONFIGURES;
      } else {
        // Default NFRs to verification gate
        req.taskPattern = TaskPattern.VERIFICATION_GATE;
        req.needsWorktree = false;
        req.dependsOn = req.explicitVerifies.length > 0 ? req.explicitVerifies : allFRs;
        req.dependencyType = DependencyType.VERIFIES;
      }
      break;

    case 'User Story':
      // User stories may aggregate FRs OR introduce new logic
      // Check if this US introduces new functionality not in FRs
      const newLogic = detectNewLogicInUserStory(req, allRequirements);
      
      if (newLogic.hasNewLogic) {
        // Heavy US: Treat as FR with full TDD chain
        req.taskPattern = TaskPattern.TDD_CHAIN;
        req.needsWorktree = true;
        req.dependsOn = ['approvalGate'];
        req.dependencyType = DependencyType.BLOCKS;
        req.newLogicDescription = newLogic.description;
        console.log(`    ⚠️  US ${req.originalId} introduces new logic: ${newLogic.description}`);
      } else {
        // Light US: Pure aggregation
        req.taskPattern = TaskPattern.AGGREGATION_TASK;
        req.needsWorktree = true;
        
        // Use explicit aggregates list if provided
        if (req.explicitAggregates.length > 0) {
          req.dependsOn = req.explicitAggregates;
        } else {
          // Infer from criteria: look for references to functions like "add()", "subtract()"
          req.dependsOn = inferFRsFromCriteria(req, allRequirements);
        }
        req.dependencyType = DependencyType.AGGREGATES;
        req.aggregatedFRs = req.dependsOn;
      }
      break;

    case 'User Story Integration':
      // US-INT: integration verification — treated as aggregation task with runtime verification
      req.taskPattern = TaskPattern.AGGREGATION_TASK;
      req.needsWorktree = true;
      req.dependencyType = DependencyType.AGGREGATES;
      
      // US-INT aggregates via explicitAggregates or explicitVerifies
      if (req.explicitAggregates.length > 0) {
        req.dependsOn = req.explicitAggregates;
      } else if (req.explicitVerifies.length > 0) {
        req.dependsOn = req.explicitVerifies;
      } else {
        req.dependsOn = allFRs.map(r => r.originalId);
      }
      req.aggregatedFRs = req.dependsOn;
      console.log(`    🔗 US-INT ${req.originalId} aggregates ${req.dependsOn.length} FRs`);
      break;

    case 'Setup':
    case 'Docs':
    case 'QA':
      // Setup and docs can be parallel
      req.taskPattern = TaskPattern.CONFIGURATION_TASK;
      req.needsWorktree = req.type === 'Setup'; // Setup might need worktree
      req.dependsOn = ['approvalGate'];
      req.dependencyType = DependencyType.CONFIGURES;
      break;

    default:
      // Default to TDD chain for unknown types
      req.taskPattern = TaskPattern.TDD_CHAIN;
      req.needsWorktree = true;
      req.dependsOn = ['approvalGate'];
      req.dependencyType = DependencyType.BLOCKS;
  }
}

/**
 * Determine if NFR is a verification type (needs to run after FRs)
 */
function isVerificationNFR(req) {
  const verificationKeywords = [
    'coverage', 'test coverage', 'code coverage',
    'quality', 'lint', 'formatting',
    'documentation', 'docs completeness',
    'performance', 'benchmark', 'speed',
    'security', 'audit', 'compliance',
    'metrics', 'measurement'
  ];
  
  const text = (req.title + ' ' + req.description).toLowerCase();
  return verificationKeywords.some(kw => text.includes(kw));
}

/**
 * Determine if NFR is a configuration type (can run in parallel)
 */
function isConfigurationNFR(req) {
  const configKeywords = [
    'module system', 'package', 'build',
    'configuration', 'setup', 'init',
    'typescript config', 'webpack', 'vite',
    'eslint', 'prettier', 'ci/cd',
    'monorepo', 'workspace', 'turbo',
    'tsconfig', 'jest', 'vitest'
  ];

  const text = (req.title + ' ' + req.description).toLowerCase();
  return configKeywords.some(kw => text.includes(kw));
}

/**
 * Detect if User Story introduces new logic not present in FRs
 * Returns { hasNewLogic: boolean, description: string }
 */
function detectNewLogicInUserStory(us, allFRs) {
  const text = (us.title + ' ' + us.description + ' ' + us.acceptanceCriteria.join(' ')).toLowerCase();

  // Keywords that suggest NEW logic (not just calling existing functions)
  const newLogicKeywords = [
    { pattern: /\bparse\b|\bparsing\b/, desc: 'expression parsing' },
    { pattern: /\bevaluate\b|\bevaluation\b|\beval\b/, desc: 'expression evaluation' },
    { pattern: /\btransform\b|\btransformation\b/, desc: 'data transformation' },
    { pattern: /\bcalculate\s+(?:complex|expression|formula)/, desc: 'complex calculation' },
    { pattern: /\bvalidate\s+(?:input|format|syntax)/, desc: 'input validation logic' },
    { pattern: /\bchain\b|\bcompose\b|\bpipe\b/, desc: 'function composition' },
    { pattern: /\basync\b|\bconcurrent\b|\bparallel\b/, desc: 'async/concurrent logic' },
    { pattern: /\bcache\b|\bcaching\b/, desc: 'caching logic' },
    { pattern: /\bserialize\b|\bdeserialize\b/, desc: 'serialization logic' },
    { pattern: /\bencode\b|\bdecode\b/, desc: 'encoding/decoding logic' },
    { pattern: /\bschedule\b|\btimer\b|\bcron\b/, desc: 'scheduling logic' },
    { pattern: /\bsocket\b|\bstream\b|\bevent\s+(?:stream|source)/, desc: 'streaming/event logic' },
    { pattern: /\bcompile\b|\binterpret\b/, desc: 'compilation logic' }
  ];

  // Check for new logic keywords
  for (const { pattern, desc } of newLogicKeywords) {
    if (pattern.test(text)) {
      return { hasNewLogic: true, description: desc };
    }
  }

  // Build list of known operations from FRs
  const knownOperations = new Set();
  allFRs
    .filter(r => r.type === 'FR')
    .forEach(r => {
      // Extract operation names from FR titles
      const titleLower = r.title.toLowerCase();
      if (titleLower.includes('addition')) knownOperations.add('add');
      if (titleLower.includes('subtraction')) knownOperations.add('subtract');
      if (titleLower.includes('multiplication')) knownOperations.add('multiply');
      if (titleLower.includes('division')) knownOperations.add('divide');
      if (titleLower.includes('validation')) knownOperations.add('validate');
      
      // Add explicit functions
      if (r.functions) {
        r.functions.forEach(f => knownOperations.add(f.toLowerCase()));
      }
    });

  // Check if US mentions operations in a way that suggests AGGREGATION
  // vs introducing NEW logic
  const aggregationPhrases = [
    /can\s+(?:import|use|call)\s+/,
    /access\s+to\s+/,
    /expose\s+/,
    /barrel\s+export/,
    /combine\s+/,
    /aggregate\s+/
  ];
  
  const isAggregation = aggregationPhrases.some(pattern => pattern.test(text));
  
  // If it looks like aggregation and doesn't have strong new logic signals, treat as light
  if (isAggregation && !text.match(/\bimplement\b|\bcreate\b|\bbuild\b|\bdevelop\b/)) {
    return { hasNewLogic: false, description: '' };
  }

  // Look for explicit function calls with parentheses
  const functionCalls = text.match(/(\w+)\s*\(/g) || [];
  const uniqueCalls = [...new Set(functionCalls.map(f => f.replace('(', '').toLowerCase()))];

  // Common words to ignore
  const ignoreWords = new Set(['if', 'while', 'for', 'switch', 'catch', 'console', 'log', 'error',
    'import', 'export', 'from', 'return', 'await', 'async', 'try', 'throw', 'const', 'let', 'var',
    'function', 'class', 'new', 'this', 'true', 'false', 'null', 'undefined', 'object', 'array']);

  // Check for truly unknown functions
  const unknownFunctions = uniqueCalls.filter(fn =>
    !knownOperations.has(fn) &&
    !ignoreWords.has(fn) &&
    fn.length > 2 &&
    !fn.match(/^(get|set|is|has|can|should)/)  // Ignore common prefixes
  );

  // Need multiple unknown functions to trigger heavy classification
  if (unknownFunctions.length >= 3) {
    return {
      hasNewLogic: true,
      description: `unknown operations: ${unknownFunctions.join(', ')}`
    };
  }

  return { hasNewLogic: false, description: '' };
}

/**
 * Infer FR dependencies from User Story criteria
 */
function inferFRsFromCriteria(us, allRequirements) {
  const frs = [];
  const criteriaText = us.acceptanceCriteria.join(' ').toLowerCase();
  const descriptionText = us.description.toLowerCase();
  const fullText = criteriaText + ' ' + descriptionText;
  
  // Look for function references like "add()", "subtract()"
  const functionPattern = /(\w+)\s*\(/g;
  let match;
  while ((match = functionPattern.exec(fullText)) !== null) {
    const funcName = match[1];
    // Find FR that implements this function
    const implementingFR = allRequirements.find(r => 
      r.type === 'FR' && 
      (r.functions?.includes(funcName) || 
       r.title.toLowerCase().includes(funcName))
    );
    if (implementingFR && !frs.includes(implementingFR.originalId)) {
      frs.push(implementingFR.originalId);
    }
  }
  
  // If no functions found, default to all FRs
  if (frs.length === 0) {
    return allRequirements
      .filter(r => r.type === 'FR')
      .map(r => r.originalId);
  }
  
  return frs;
}

/**
 * Detect cross-cutting concerns: FRs that modify the same files as other FRs.
 * These should run AFTER the foundational FRs to avoid concurrent modification.
 * @returns {Map<string, string[]>} Map of reqId -> array of reqIds it should depend on
 */
function detectCrossCuttingConcerns(requirements) {
  const crossCuttingDeps = new Map();
  
  // Normalize file path: strip backticks and function suffixes
  function normalizeFile(file) {
    return file
      .replace(/^`|`$/g, '')         // Strip backticks
      .replace(/:\w+\(\)$/, '')     // Strip :function() suffix
      .trim();
  }
  
  // Group FRs by their normalized target files
  const fileToFRs = new Map();
  for (const req of requirements) {
    if (req.type !== 'FR' || !req.files || req.files.length === 0) continue;
    
    for (const file of req.files) {
      const normalized = normalizeFile(file);
      if (!normalized) continue;
      if (!fileToFRs.has(normalized)) {
        fileToFRs.set(normalized, new Map()); // Use Map to deduplicate by req ID
      }
      fileToFRs.get(normalized).set(req.id, req);
    }
  }
  
  // For each file shared by multiple FRs, the non-foundational ones
  // (those with "validate", "check", "verify" in title) should depend on others
  for (const [file, reqMap] of fileToFRs) {
    const frs = Array.from(reqMap.values());
    if (frs.length <= 1) continue; // No conflict
    
    // Identify foundational FRs (basic operations) vs cross-cutting FRs
    const foundational = frs.filter(r => 
      !/validat|check|verify|audit|enforce|guard/i.test(r.title)
    );
    const crossCutting = frs.filter(r => 
      /validat|check|verify|audit|enforce|guard/i.test(r.title)
    );
    
    if (crossCutting.length === 0) continue;
    
    // Cross-cutting FRs depend on foundational FRs for same file
    for (const ccFR of crossCutting) {
      const deps = crossCuttingDeps.get(ccFR.id) || [];
      for (const baseFR of foundational) {
        if (baseFR.id !== ccFR.id && !deps.includes(baseFR.originalId)) {
          deps.push(baseFR.originalId);
        }
      }
      if (deps.length > 0) {
        crossCuttingDeps.set(ccFR.id, deps);
        console.log(`    🔗 Cross-cutting: ${ccFR.originalId} depends on ${deps.join(', ')} (shared file: ${file})`);
      }
    }
  }
  
  return crossCuttingDeps;
}

/**
 * Build semantic dependency graph
 */
function buildSemanticDependencyGraph(requirements, implicitDeps) {
  const graph = {
    nodes: requirements.map(r => ({
      id: r.id,
      originalId: r.originalId,
      type: r.type,
      taskPattern: r.taskPattern,
      needsWorktree: r.needsWorktree,
    })),
    edges: [],
    phases: []
  };
  
  // Add explicit dependencies
  for (const req of requirements) {
    for (const depId of req.dependsOn || []) {
      graph.edges.push({
        from: depId,
        to: req.originalId,
        type: req.dependencyType,
      });
    }
  }
  
  // Add implicit dependencies from text analysis
  for (const [reqId, deps] of Object.entries(implicitDeps)) {
    for (const depId of deps) {
      // Avoid duplicates
      const exists = graph.edges.some(e => 
        e.from === depId && e.to === reqId
      );
      if (!exists) {
        graph.edges.push({
          from: depId,
          to: reqId,
          type: DependencyType.BLOCKS,
        });
      }
    }
  }
  
  // Compute phases based on dependency depth
  graph.phases = computeExecutionPhases(graph);
  
  return graph;
}

/**
 * Compute execution phases based on dependency graph
 */
function computeExecutionPhases(graph) {
  const phases = [];
  const visited = new Set();
  
  // Phase 1: Foundation (no dependencies except approval)
  const foundation = graph.nodes.filter(n => {
    const deps = graph.edges.filter(e => e.to === n.originalId);
    return deps.length === 0 || 
           (deps.length === 1 && deps[0].from === 'APPROVAL');
  });
  phases.push({
    name: 'Foundation',
    type: 'PARALLEL_WORKTREES',
    nodes: foundation.map(n => n.originalId),
  });
  foundation.forEach(n => visited.add(n.originalId));
  
  // Phase 2: Integration (depends on foundation)
  const integration = graph.nodes.filter(n => {
    if (visited.has(n.originalId)) return false;
    const deps = graph.edges.filter(e => 
      e.to === n.originalId && 
      !visited.has(e.from) &&
      e.from !== 'APPROVAL'
    );
    return deps.length === 0 || deps.every(d => visited.has(d.from));
  });
  if (integration.length > 0) {
    phases.push({
      name: 'Integration',
      type: 'SEQUENTIAL_WORKTREES',
      nodes: integration.map(n => n.originalId),
    });
    integration.forEach(n => visited.add(n.originalId));
  }
  
  // Phase 3: Verification gates (no worktrees)
  const verification = graph.nodes.filter(n => 
    !visited.has(n.originalId) && !n.needsWorktree
  );
  if (verification.length > 0) {
    phases.push({
      name: 'Quality Verification',
      type: 'VERIFICATION_GATES',
      nodes: verification.map(n => n.originalId),
    });
    verification.forEach(n => visited.add(n.originalId));
  }
  
  return phases;
}

// ... existing helper functions remain unchanged ...

function extractIntegrationPoints(prdContent) {
  const points = [];
  const sectionPattern = /#{1,3}\s*5\.2|#{1,3}\s*Integration Points/i;
  const sectionMatch = prdContent.match(sectionPattern);

  if (sectionMatch) {
    const startIdx = sectionMatch.index;
    const headerEndMatch = prdContent.slice(startIdx).match(/\n/);
    const contentStartIdx = headerEndMatch
      ? startIdx + headerEndMatch.index + 1
      : startIdx;
    const sectionEndMatch = prdContent
      .slice(contentStartIdx)
      .match(/^#{1,3}\s[^#]/m);
    const sectionEnd = sectionEndMatch
      ? contentStartIdx + sectionEndMatch.index
      : prdContent.length;
    const sectionContent = prdContent.slice(contentStartIdx, sectionEnd);

    const pointPattern =
      /\*\*Point\s+(\d+):\s*([^*]+?)\*\*\s*\(`([^`]+)`\)\s*\n([\s\S]*?)(?=\*\*Point|\n#{1,3}|\n\n\*\*|$)/g;

    let pointMatch;
    while (true) {
      pointMatch = pointPattern.exec(sectionContent);
      if (pointMatch === null) break;

      const pointNumber = parseInt(pointMatch[1]);
      const title = pointMatch[2].trim();
      const location = pointMatch[3].trim();
      const details = pointMatch[4].trim();

      const locMatch = location.match(/([\w/]+\.\w+):(\w+)\(\)/);
      const file = locMatch ? locMatch[1] : '';
      const func = locMatch ? locMatch[2] : '';

      const reqMatch = details.match(
        /\*\*?Requirements?:\*\*?\s*([A-Z0-9a-z\-,.\s]+)/i,
      );
      const requirements = reqMatch
        ? reqMatch[1]
            .split(/[,\s]+/)
            .map((r) => r.trim())
            .filter((r) => /^[A-Z]+-\d+[a-z]?$/i.test(r))
        : [];

      points.push({
        pointNumber,
        title,
        location,
        file,
        function: func,
        details,
        requirements,
      });
    }
  }

  return points;
}

function extractDataModels(prdContent) {
  const models = [];
  const sectionPattern = /#{1,3}\s*5\.6|#{1,3}\s*Data Models/i;
  const sectionMatch = prdContent.match(sectionPattern);

  if (sectionMatch) {
    const startIdx = sectionMatch.index;
    const headerEndMatch = prdContent.slice(startIdx).match(/\n/);
    const contentStartIdx = headerEndMatch
      ? startIdx + headerEndMatch.index + 1
      : startIdx;
    const sectionEndMatch = prdContent
      .slice(contentStartIdx)
      .match(/^#{1,3}\s[^#]/m);
    const sectionEnd = sectionEndMatch
      ? contentStartIdx + sectionEndMatch.index
      : prdContent.length;
    const sectionContent = prdContent.slice(contentStartIdx, sectionEnd);

    const modelPattern =
      /\*\*([^*(]+)\*\*\s*\([^)]*Requirements?:\s*([A-Z0-9a-z\-,.\s]+)\)\s*\n?```(\w+)\n([\s\S]*?)```/g;

    let modelMatch;
    while (true) {
      modelMatch = modelPattern.exec(sectionContent);
      if (modelMatch === null) break;

      const name = modelMatch[1].trim();
      const reqString = modelMatch[2] || '';
      const type = modelMatch[3];
      const definition = modelMatch[4].trim();

      const requirements = reqString
        .split(/[,\s]+/)
        .map((r) => r.trim())
        .filter((r) => /^[A-Z]+-\d+[a-z]?$/i.test(r));

      models.push({
        name,
        type: type === 'go' ? 'go-struct' : `${type}-type`,
        definition,
        requirements,
      });
    }
  }

  const structPattern = /type\s+(\w+)\s+struct\s*\{([^}]+)\}/gs;
  let structMatch;
  while (true) {
    structMatch = structPattern.exec(prdContent);
    if (structMatch === null) break;

    const name = structMatch[1];
    if (!models.find((m) => m.name === name)) {
      models.push({
        name,
        type: 'go-struct',
        definition: `type ${name} struct {${structMatch[2]}}`,
        requirements: [],
      });
    }
  }

  return models;
}

function extractConfiguration(prdContent) {
  const configs = [];
  const sectionPattern = /#{1,3}\s*5\.4|#{1,3}\s*Configuration/i;
  const sectionMatch = prdContent.match(sectionPattern);

  if (sectionMatch) {
    const startIdx = sectionMatch.index;
    const headerEndMatch = prdContent.slice(startIdx).match(/\n/);
    const contentStartIdx = headerEndMatch
      ? startIdx + headerEndMatch.index + 1
      : startIdx;
    const sectionEndMatch = prdContent
      .slice(contentStartIdx)
      .match(/^#{1,3}\s[^#]/m);
    const sectionEnd = sectionEndMatch
      ? contentStartIdx + sectionEndMatch.index
      : prdContent.length;
    const sectionContent = prdContent.slice(contentStartIdx, sectionEnd);

    const configPattern =
      /\|\s*`?([^`|]+)`?\s*\|\s*(\w+)\s*\|\s*([^|]+)\|\s*([A-Z0-9-,\s]+)\s*\|/g;

    let configMatch;
    while (true) {
      configMatch = configPattern.exec(sectionContent);
      if (configMatch === null) break;

      const key = configMatch[1].trim();
      const type = configMatch[2].trim();
      const default_ = configMatch[3].trim();
      const reqString = configMatch[4].trim();

      const requirements = reqString
        .split(/[,\s]+/)
        .map((r) => r.trim())
        .filter((r) => /^[A-Z]+-\d+$/i.test(r));

      if (key && requirements.length > 0) {
        configs.push({
          key,
          type,
          default: default_,
          requirements,
        });
      }
    }
  }

  return configs;
}

function extractArchitectureSection(prdContent) {
  const sectionPattern = /#{1,3}\s*5\.1|#{1,3}\s*Architecture/i;
  const match = prdContent.match(sectionPattern);

  if (match) {
    const startIdx = match.index;
    const sectionEndMatch = prdContent.slice(startIdx).match(/^#{1,3}\s[^#]/m);
    const sectionEnd = sectionEndMatch
      ? startIdx + sectionEndMatch.index
      : prdContent.length;
    return prdContent.slice(startIdx, sectionEnd).trim();
  }

  return '';
}

function estimateHoursFromDescription(description) {
  if (!description) return 2;

  const desc = description.toLowerCase();
  let hours = 2;

  if (desc.includes('complex') || desc.includes('advanced')) hours += 2;
  if (desc.includes('simple') || desc.includes('basic')) hours -= 1;
  if (desc.includes('database') || desc.includes('db')) hours += 1;
  if (desc.includes('api') || desc.includes('endpoint')) hours += 1;
  if (desc.includes('ui') || desc.includes('interface')) hours += 0.5;
  if (desc.includes('test') || desc.includes('validation')) hours += 0.5;

  const lines = description.split('\n').length;
  if (lines > 10) hours += 1;
  if (lines > 20) hours += 1;

  return Math.max(0.5, Math.min(hours, 8));
}

function detectDependencies(requirements) {
  const dependencies = {};

  for (const req of requirements) {
    const deps = [];
    const text = (req.description || '').toLowerCase();

    for (const otherReq of requirements) {
      if (otherReq.id === req.id) continue;

      const patterns = [
        new RegExp(`\\b${otherReq.id}\\b`, 'i'),
        new RegExp(`\\b${otherReq.title.toLowerCase().substring(0, 20).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`, 'i'),
      ];

      for (const pattern of patterns) {
        if (pattern.test(text)) {
          deps.push(otherReq.id);
          break;
        }
      }
    }

    if (deps.length > 0) {
      dependencies[req.id] = deps;
    }
  }

  return dependencies;
}

function extractImplementationMapping(prdContent) {
  const mappings = {};
  const sectionPattern = /#{1,3}\s*5\.8|#{1,3}\s*Implementation Mapping/i;
  const sectionMatch = prdContent.match(sectionPattern);

  if (sectionMatch) {
    const startIdx = sectionMatch.index;
    const headerEndMatch = prdContent.slice(startIdx).match(/\n/);
    const contentStartIdx = headerEndMatch
      ? startIdx + headerEndMatch.index + 1
      : startIdx;
    const sectionEndMatch = prdContent
      .slice(contentStartIdx)
      .match(/^#{1,3}\s[^#]/m);
    const sectionEnd = sectionEndMatch
      ? contentStartIdx + sectionEndMatch.index
      : prdContent.length;
    const sectionContent = prdContent.slice(contentStartIdx, sectionEnd);

    const rowPattern =
      /\|\s*([A-Z]+-\d+[a-z]?)\s*\|\s*([^|]*)\|\s*([^|]*)\|\s*([^|]*)\|/g;

    let rowMatch;
    while (true) {
      rowMatch = rowPattern.exec(sectionContent);
      if (rowMatch === null) break;

      const reqId = rowMatch[1].trim().toLowerCase();
      const filesStr = rowMatch[2].trim();
      const pointsStr = rowMatch[3].trim();
      const modelsStr = rowMatch[4].trim();

      mappings[reqId] = {
        files: filesStr
          .split(/[,\s]+/)
          .map((f) => f.trim())
          .filter(Boolean),
        integrationPoints: pointsStr
          .split(/[,\s]+/)
          .map((p) => p.trim())
          .filter(Boolean),
        dataModels: modelsStr
          .split(/[,\s]+/)
          .map((m) => m.trim())
          .filter(Boolean),
        explicitMappingExists: true,
      };
    }
  }

  return mappings;
}

function validateRequirements(requirements) {
  const warnings = [];

  for (const req of requirements) {
    const hasFiles = req.files && req.files.length > 0;
    const hasIntegrationPoints =
      req.integrationPoints && req.integrationPoints.length > 0;
    const hasDataModels = req.dataModels && req.dataModels.length > 0;
    const hasExplicitMapping = req.explicitMappingExists === true;

    if (req.type === 'Setup' || req.type === 'Docs' || req.type === 'QA') {
      continue;
    }

    if (
      !hasFiles &&
      !hasIntegrationPoints &&
      !hasDataModels &&
      !hasExplicitMapping
    ) {
      warnings.push(
        `${req.originalId}: No implementation target found. ` +
          `Add to Section 5.8 (Implementation Mapping) or reference in 5.2 (Integration Points) / 5.6 (Data Models)`,
      );
      
      req.files = req.files || ['TBD'];
    }
  }

  if (warnings.length > 0) {
    console.warn('\n⚠️  PRD Validation Warnings:');
    console.warn(warnings.join('\n'));
    console.warn('\nProceeding with placeholder files (TBD)...\n');
  }
  
  return warnings;
}

/**
 * Validates that no two FRs share the same target file.
 * Shared target files cause add/add merge conflicts in Hydra's DRYAD pipeline.
 * @param {Array} requirements - Parsed requirements
 * @returns {Array} Array of error strings (empty if no overlaps)
 */
function validateFileOverlap(requirements) {
  const errors = [];
  const fileToFRs = new Map();

  // Normalize file path: strip backticks, quotes, function suffixes
  function normalizeFile(file) {
    return file
      .replace(/^`|`$/g, '')
      .replace(/^"|"$/g, '')
      .replace(/:\w+\(\)$/, '')
      .trim();
  }

  // Map each file to the FRs that claim it
  for (const req of requirements) {
    if (req.type !== 'FR' || !req.files || req.files.length === 0) continue;
    for (const file of req.files) {
      const normalized = normalizeFile(file);
      if (!normalized || normalized === 'TBD') continue;
      if (!fileToFRs.has(normalized)) {
        fileToFRs.set(normalized, []);
      }
      fileToFRs.get(normalized).push(req.originalId);
    }
  }

  // Find files claimed by more than one FR. FRs that share a file are
  // acceptable when the cross-cutting detector has serialized them: one FR
  // depends on the other claimants for that file, so no merge conflict can
  // occur. Only unserialized overlap is an error.
  for (const [file, frIds] of fileToFRs) {
    if (frIds.length < 2) continue;
    const byId = new Map(requirements.map((r) => [r.originalId, r]));
    const serialized = frIds.some((consumerId) => {
      const req = byId.get(consumerId);
      const deps = [...(req?.explicitDependsOn || []), ...(req?.dependsOn || [])];
      return frIds.every((frId) => frId === consumerId || deps.includes(frId));
    });
    if (!serialized) {
      errors.push(`  File "${file}" is targeted by multiple FRs: ${frIds.join(', ')}`);
    }
  }

  return errors;
}

function extractRequirementContextEnhanced(req, prdContent, globalContext) {
  const fileFuncPattern = /`?([\w/]+\.(?:go|js|ts|py|java|rs)):(\w+)\(\)`?/g;
  let fileMatch;
  while (true) {
    fileMatch = fileFuncPattern.exec(req.description);
    if (fileMatch === null) break;
    if (!req.files.includes(fileMatch[1])) {
      req.files.push(fileMatch[1]);
    }
    if (!req.functions.includes(fileMatch[2])) {
      req.functions.push(fileMatch[2]);
    }
  }

  const implementationMapping = extractImplementationMapping(prdContent);
  if (implementationMapping[req.originalId.toLowerCase()]) {
    const mapping = implementationMapping[req.originalId.toLowerCase()];
    req.explicitMappingExists = true;

    for (const file of mapping.files) {
      if (!req.files.includes(file)) {
        req.files.push(file);
      }
    }
  }

  for (const point of globalContext.integrationPoints) {
    if (point.requirements.includes(req.originalId)) {
      if (
        !req.integrationPoints.find((p) => p.pointNumber === point.pointNumber)
      ) {
        req.integrationPoints.push(point);
        if (point.file && !req.files.includes(point.file)) {
          req.files.push(point.file);
        }
        if (point.function && !req.functions.includes(point.function)) {
          req.functions.push(point.function);
        }
      }
    }
  }

  for (const model of globalContext.dataModels) {
    if (model.requirements.includes(req.originalId)) {
      if (!req.dataModels.find((m) => m.name === model.name)) {
        req.dataModels.push(model);
      }
    }
  }

  for (const config of globalContext.configuration) {
    if (config.requirements.includes(req.originalId)) {
      if (!req.configuration.includes(config.key)) {
        req.configuration.push(config.key);
      }
    }
  }
}

export {
  extractIntegrationPoints,
  extractDataModels,
  extractConfiguration,
  extractImplementationMapping,
  validateRequirements,
};

export { analyzePRDDeterministic as default };
