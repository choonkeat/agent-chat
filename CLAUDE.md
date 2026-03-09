# Agent Chat

## Testing

Always use `make test` to run tests. Never run `go test` or `go vet` directly.

## E2E Testing

Run `make e2e` to execute Playwright E2E tests and serve the HTML report.

- Tests connect to a remote Chrome via CDP (`CDP_ENDPOINT`, default `http://chrome:9223`)
- Set `SLOW_MO=500` to slow down browser actions for live viewing in Agent View
- After tests complete, the HTML report is served on `E2E_REPORT_PORT` (defaults to `$PORT` or 3001)
- View the report in the Preview tab

Example: `SLOW_MO=500 make e2e`
