// Entry point for canvas-bundle.js
// Bundles AgentWhiteboard + validateInstructions into a single IIFE
export { AgentWhiteboard } from '/repos/agent-whiteboard/workspace/src/index.js';
export { validateInstructions, formatValidationErrors } from '/repos/agent-whiteboard/workspace/mcp-client/validate-instructions.js';
