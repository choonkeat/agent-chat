#!/usr/bin/env node
//
// prepare-local-build.js — runs on `npm link` / `npm install` from source.
// Builds the Go binary for the current platform into npm-platforms/ so the
// JS wrapper (bin/agent-chat.js) picks up the local build instead of falling
// back to a potentially stale npm-installed platform package.
//
// Skips silently when:
//   - No Go toolchain available (e.g. registry install via npm)
//   - No main.go present (not a source checkout)
//

import { execSync } from "child_process";
import { existsSync, mkdirSync } from "fs";
import { join, dirname } from "path";
import { fileURLToPath } from "url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(__dirname, "..");

// Only run from a source checkout (main.go exists)
if (!existsSync(join(repoRoot, "main.go"))) {
  process.exit(0);
}

// Check for Go toolchain
try {
  execSync("go version", { stdio: "ignore" });
} catch {
  process.exit(0);
}

const PLATFORM_MAP = { linux: "linux", darwin: "darwin", win32: "windows" };
const ARCH_MAP = { x64: "amd64", arm64: "arm64" };
const PKG_SUFFIX_MAP = { linux: "linux", darwin: "darwin", win32: "win32" };

const platform = PLATFORM_MAP[process.platform];
const arch = ARCH_MAP[process.arch];
const pkgPlatform = PKG_SUFFIX_MAP[process.platform];

if (!platform || !arch || !pkgPlatform) {
  console.warn(`prepare: unsupported platform ${process.platform}-${process.arch}, skipping`);
  process.exit(0);
}

const suffix = `${pkgPlatform}-${process.arch}`;
const binDir = join(repoRoot, "npm-platforms", suffix, "bin");
const binName = process.platform === "win32" ? "agent-chat.exe" : "agent-chat";
const binPath = join(binDir, binName);

// Skip if already built
if (existsSync(binPath)) {
  process.exit(0);
}

console.log(`prepare: building agent-chat for ${platform}/${arch}...`);
mkdirSync(binDir, { recursive: true });

try {
  execSync(
    `CGO_ENABLED=0 go build -trimpath -o ${JSON.stringify(binPath)} .`,
    { cwd: repoRoot, stdio: "inherit" }
  );
  console.log(`prepare: built ${binPath}`);
} catch (e) {
  console.warn(`prepare: go build failed (${e.message}), skipping`);
}
