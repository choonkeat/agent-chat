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

- Tests connect to a remote Chrome via CDP (`CDP_ENDPOINT`, default `http://chrome:9223`)
- Set `SLOW_MO=500` to slow down browser actions for live viewing in Agent View
- `make e2e-report` kills any previous report server, then serves on `E2E_REPORT_PORT` (defaults to `$PORT` or 3001)
- View the report in the Preview tab

Example: `SLOW_MO=500 make e2e-test && make e2e-report`
