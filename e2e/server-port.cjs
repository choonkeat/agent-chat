// @ts-check
// Which port a spec's test server must bind.
//
// The CDP browser lives in another container and reaches this one through a
// per-session tunnel that forwards exactly ONE port: this session's preview
// port, $PORT. Everything else is unreachable, so a server on port 0 (a fresh
// ephemeral port) makes every page.goto fail with ERR_CONNECTION_RESET before
// it touches the app — in specs that have nothing to do with the change under
// test. That is the "known flake" this suite has been living with.
//
// Measured 2026-08-26 with identical agent-chat servers, each answering 200
// locally, navigated from the shared browser:
//   http://localhost:3003  ($PORT)              -> loads
//   http://localhost:3000, :3002, :3004, :3010  -> ERR_CONNECTION_RESET
//   http://localhost:5003  ($PUBLIC_PORT)       -> ERR_CONNECTION_REFUSED
//   http://172.18.0.2:3004 (container IP)       -> timeout, not routable
// So "any free port in SWE_PREVIEW_PORTS (3000-3019)" is wrong: that range is
// the pool sessions are allotted from, one port each — not a range this
// container may spread across.
//
// Consequences:
//   - playwright.config.cjs pins workers to 1 while pinned: one port, one
//     server at a time.
//   - a spec cannot hold two browser-reachable servers at once; see
//     msg-style-persist.spec.cjs, which reuses the one port sequentially.
//
// Escape hatches: E2E_SERVER_PORT=<n> to pin elsewhere, E2E_SERVER_PORT=0 to
// restore ephemeral ports where every port is reachable (no tunnel).

/** The port a test server should bind, as a string for AGENT_CHAT_PORT. */
function serverPort() {
  const override = process.env.E2E_SERVER_PORT;
  if (override !== undefined && override !== '') return String(override);
  return process.env.PORT || '0';
}

/** True when every server lands on one shared port, so they cannot overlap. */
function isPinned() {
  return serverPort() !== '0';
}

module.exports = { serverPort, isPinned };
