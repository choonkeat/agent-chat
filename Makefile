build: bundle-client
	go build -o dist/agent-chat .

bundle-client:
	npx esbuild canvas-entry.ts --bundle --format=iife --global-name=CanvasBundle --outfile=client-dist/canvas-bundle.js --target=es2020

test:
	go vet ./...
	go test ./...
