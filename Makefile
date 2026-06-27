.PHONY: build bundle-client publish publish-dry test unit-test e2e-test e2e-report bump

build: build-platforms
	npm config set prefix $(HOME)/.swe-swe 2>/dev/null; npm link 2>/dev/null || true

bundle-client:
	npx esbuild canvas-entry.ts --bundle --format=iife --global-name=CanvasBundle --outfile=client-dist/canvas-bundle.js --target=es2020

test: unit-test e2e-test

unit-test:
	go vet ./...
	go test ./...

e2e-test: build-platforms
	SLOW_MO=$${SLOW_MO:-0} npx playwright test -c playwright.config.cjs

e2e-report:
	E2E_REPORT_PORT=$${E2E_REPORT_PORT:-$${PORT:-3001}}; \
	lsof -ti tcp:$$E2E_REPORT_PORT | xargs -r kill 2>/dev/null || true; \
	echo "Serving HTML report on http://localhost:$$E2E_REPORT_PORT"; \
	npx -y http-server playwright-report -p $$E2E_REPORT_PORT --host 0.0.0.0 -c-1

build-platforms: bundle-client
	./scripts/build-platforms.sh

publish-dry: build-platforms
	DRY_RUN=true ./scripts/publish.sh

publish: build-platforms
	@# Binaries are built by the build-platforms prerequisite above. Only now,
	@# right before publishing, prompt for the npm OTP if it isn't already in
	@# the environment — so the short-lived OTP isn't burned during the build.
	@if [ -n "$$NPM_OTP" ]; then \
		DRY_RUN=false ./scripts/publish.sh; \
	else \
		read -rsp "Enter npm OTP (blank to publish without one): " otp; echo; \
		DRY_RUN=false NPM_OTP="$$otp" ./scripts/publish.sh; \
	fi

bump:
	@if [ -z "$(VERSION)" ]; then \
		echo "Usage: make bump VERSION=x.y.z"; \
		exit 1; \
	fi
	@echo "Bumping version to $(VERSION)..."
	@node -e 'var fs=require("fs"),p=JSON.parse(fs.readFileSync("package.json","utf8"));p.version="$(VERSION)";for(var k of Object.keys(p.optionalDependencies||{}))p.optionalDependencies[k]="$(VERSION)";fs.writeFileSync("package.json",JSON.stringify(p,null,2)+"\n")'
	@echo "Version bumped to $(VERSION)"
