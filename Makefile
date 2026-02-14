.PHONY: build bundle-client publish publish-dry test bump

build: bundle-client
	go build -o dist/agent-chat .

bundle-client:
	npx esbuild canvas-entry.ts --bundle --format=iife --global-name=CanvasBundle --outfile=client-dist/canvas-bundle.js --target=es2020

test:
	go vet ./...
	go test ./...

build-platforms: bundle-client
	./scripts/build-platforms.sh

publish-dry: build-platforms
	DRY_RUN=true ./scripts/publish.sh

publish: build-platforms
	DRY_RUN=false ./scripts/publish.sh

bump:
	@if [ -z "$(VERSION)" ]; then \
		echo "Usage: make bump VERSION=x.y.z"; \
		exit 1; \
	fi
	@echo "Bumping version to $(VERSION)..."
	@sed -i 's/"version": "[^"]*"/"version": "$(VERSION)"/' package.json
	@sed -i 's/Version: "[^"]*"/Version: "$(VERSION)"/' main.go
	@echo "Version bumped to $(VERSION)"
