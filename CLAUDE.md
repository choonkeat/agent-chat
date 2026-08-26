# Agent Chat

## Testing

Always use `make test` to run all tests (unit + E2E). Never run `go test` or `go vet` directly.

- `make test` — runs both unit tests and E2E tests
- `make unit-test` — runs only Go unit tests (`go vet` + `go test`)
- `make e2e-test` — runs only Playwright E2E tests
- `make e2e-report` — serves the HTML report from the last E2E run

## E2E Testing

Run `make e2e-test` to execute Playwright E2E tests. Run `make e2e-report` to
serve the HTML report from the last run.

- Tests connect to a remote Chrome via CDP. Auto-detected from `BROWSER_CDP_PORT` env var, or set `CDP_ENDPOINT` explicitly (legacy default: `http://chrome:9223`)
- **The CDP endpoint is LAZY.** Nothing listens on `BROWSER_CDP_PORT` until an MCP Playwright session has opened a page — Chrome is owned by the swe-swe MCP/screencast layer, not the test runner, so a plain `connectOverCDP` cannot start it. A cold endpoint makes every spec fail at connect with `ECONNREFUSED`. **Warm it once before `make e2e-test`** by navigating with the MCP browser tool, e.g. `mcp__swe-swe-playwright__browser_navigate { url: "https://example.com" }`. The `e2e/global-setup.cjs` hook probes CDP first and prints this exact instruction if it's cold.
- **The browser can reach exactly ONE port on this container: `$PORT`.** The CDP browser lives in another container and comes back through a per-session tunnel that forwards this session's preview port only. Measured 2026-08-26 against identical servers, each answering 200 locally: `localhost:3003` (`$PORT`) loads; `localhost:3000/:3002/:3004/:3010` give `ERR_CONNECTION_RESET`; `localhost:5003` (`$PUBLIC_PORT`) gives `ERR_CONNECTION_REFUSED`; the container IP is not routable. `SWE_PREVIEW_PORTS` (3000-3019) is the pool sessions are allotted from — one port each — not a range this container may spread across.
- **Every spec therefore binds `$PORT`**, via `e2e/server-port.cjs` (`serverPort()`), and `playwright.config.cjs` pins `workers: 1` because one port means one server at a time. `ERR_CONNECTION_RESET` across specs you never touched was this, not flakiness. Overrides: `E2E_SERVER_PORT=<n>` to pin elsewhere, `E2E_SERVER_PORT=0` for ephemeral ports where every port is reachable.
- **Free `$PORT` before running.** `make e2e-report` serves on `$PORT` too, so a report server left running blocks every test server; `e2e/global-setup.cjs` fails fast with one message naming the port instead of 23 identical startup errors.
- **A full run is serial: ~30 minutes for 167 tests.** One worker is the price of one port. `playwright.config.cjs` also allows 60s per test and `retries: 1` while pinned — about one navigation per full run still hangs or is refused after a rebind, and the retry absorbs it (reported as flaky, so a real failure still fails twice).
- Two tests need two browser-reachable servers at once and are skipped while pinned: `clear-prefix.spec.cjs` "the tick seeds the next chat", and the different-port half of `msg-style-persist.spec.cjs` (which reuses the one port and empties `localStorage` at every document start to keep the cookie the thing under test).
- Set `SLOW_MO=500` to slow down browser actions for live viewing in Agent View
- `make e2e-report` kills any previous report server, then serves on `E2E_REPORT_PORT` (defaults to `$PORT` or 3001)
- View the report in the Preview tab

Example: `SLOW_MO=500 make e2e-test && make e2e-report`
See .swe-swe/docs/AGENTS.md (if it exists) for context of this current environment
