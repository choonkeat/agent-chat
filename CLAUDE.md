# Agent Chat

## Testing

Always use `make test` to run all tests (unit + E2E). Never run `go test` or `go vet` directly.

- `make test` — runs both unit tests and E2E tests
- `make unit-test` — runs only Go unit tests (`go vet` + `go test`)
- `make e2e` — runs only Playwright E2E tests

## E2E Testing

Run `make e2e` to execute Playwright E2E tests and serve the HTML report.

- Tests connect to a remote Chrome via CDP (`CDP_ENDPOINT`, default `http://chrome:9223`)
- Set `SLOW_MO=500` to slow down browser actions for live viewing in Agent View
- After tests complete, the HTML report is served on `E2E_REPORT_PORT` (defaults to `$PORT` or 3001)
- View the report in the Preview tab

Example: `SLOW_MO=500 make e2e`
