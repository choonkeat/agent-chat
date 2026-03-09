# Vanilla JavaScript Frontend

**Date:** 2026-02-09
**Status:** Accepted

## Context

The browser client needs to render chat bubbles, handle WebSocket events,
manage voice mode, and support file uploads. The client is embedded in the Go
binary via `go:embed` and served as static files.

## Decision

Use plain JavaScript with no framework and no build step. All UI code lives in
a single `app.js` file. The only pre-built dependency is `canvas-bundle.js`
(esbuild bundle of the whiteboard renderer).

## Alternatives Considered

- **React/Vue/Svelte** — adds build toolchain, node_modules, and bundle size
  for a single-page chat UI that doesn't need component reuse or complex state.
- **Lit/Web Components** — lighter than React but still adds a dependency.

## Consequences

- Zero build step for the UI (just embed and serve).
- No framework overhead — fast load, small payload.
- `app.js` grows linearly with features (~1900 lines), but remains manageable
  for a single-purpose chat client.
- DOM manipulation is manual, which is fine for the append-only chat pattern.
- Go binary embeds the client directly — no separate frontend deploy.
