// @ts-check
// End-to-end coverage for content-addressed export asset filenames.
//
// The export_chat_md MCP tool copies attachments into agent-chats/assets/ under
// the name {date}-{NN}-{N}-{sha12}.{ext}. The sha12 (first 12 hex of the file's
// sha256) before the extension guarantees distinct content never collides on
// numbering alone. This spec drives the REAL server binary through its HTTP MCP
// transport (POST /mcp): it seeds an attachment via send_progress, triggers
// export_chat_md, then asserts the asset on disk carries the correct digest and
// that the generated .md links to that exact filename.
//
// Unlike the other specs this one needs no browser page — it talks to the
// server's HTTP MCP endpoint directly — but it still runs under the e2e runner
// so the full export pipeline (MCP → bus → disk) is exercised against the
// shipped binary, not a Go unit harness.
const { test, expect } = require('@playwright/test');
const { spawn } = require('child_process');
const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const os = require('os');

function startServer() {
  return new Promise((resolve, reject) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'agent-chat-sha-'));
    const bin = path.resolve(__dirname, '..', 'npm-platforms', 'linux-x64', 'bin', 'agent-chat');
    // Strip the live session's AGENT_CHAT_* env so the demo binary doesn't
    // inherit its port or replay its history.
    const cleanEnv = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('AGENT_CHAT_'))
    );
    cleanEnv.AGENT_CHAT_PORT = '0';

    // -no-stdio-mcp leaves HTTP MCP (POST /mcp) available, which is all we need.
    const proc = spawn(bin, ['-no-stdio-mcp'], {
      cwd: dir,
      env: cleanEnv,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    let stderr = '';
    proc.stderr.on('data', (chunk) => {
      stderr += chunk.toString();
      const match = stderr.match(/Agent Chat UI: (http:\/\/localhost:\d+)/);
      if (match) resolve({ url: match[1], proc, dir });
    });
    proc.on('error', reject);
    proc.on('exit', (code) => {
      if (!stderr.includes('Agent Chat UI:')) {
        reject(new Error(`Server exited with code ${code}. stderr: ${stderr}`));
      }
    });
    setTimeout(() => reject(new Error('Server did not start within 10s')), 10000);
  });
}

// mcpClient performs the minimal Streamable-HTTP handshake (initialize +
// initialized) and exposes call() for tools/call. The go-sdk returns responses
// as SSE frames, so we extract the JSON from the `data:` line.
async function mcpClient(base) {
  let id = 0;
  let sessionId = null;

  async function rpc(body, expectResult) {
    const headers = {
      'Content-Type': 'application/json',
      Accept: 'application/json, text/event-stream',
    };
    if (sessionId) headers['Mcp-Session-Id'] = sessionId;
    const res = await fetch(base + '/mcp', { method: 'POST', headers, body: JSON.stringify(body) });
    const sid = res.headers.get('mcp-session-id');
    if (sid) sessionId = sid;
    if (!expectResult) return null;
    const text = await res.text();
    const line = text.split('\n').find((l) => l.startsWith('data:'));
    const json = JSON.parse((line ? line.slice(5) : text).trim());
    if (json.error) throw new Error(`MCP error: ${JSON.stringify(json.error)}`);
    return json.result;
  }

  await rpc({
    jsonrpc: '2.0', id: ++id, method: 'initialize',
    params: { protocolVersion: '2024-11-05', capabilities: {}, clientInfo: { name: 'e2e', version: '1' } },
  }, true);
  await rpc({ jsonrpc: '2.0', method: 'notifications/initialized' }, false);

  return {
    call: (name, args) => rpc(
      { jsonrpc: '2.0', id: ++id, method: 'tools/call', params: { name, arguments: args } },
      true,
    ),
  };
}

test.describe('export_chat_md — content-addressed asset filenames', () => {
  /** @type {{ url: string, proc: import('child_process').ChildProcess, dir: string } | null} */
  let server = null;

  test.beforeAll(async () => {
    server = await startServer();
  });

  test.afterAll(async () => {
    if (server?.proc) {
      server.proc.kill('SIGTERM');
      fs.rmSync(server.dir, { recursive: true, force: true });
    }
  });

  test('asset filename carries the sha256-12 of its content before the extension', async () => {
    // A unique-content PNG so the digest is deterministic and distinctive.
    const bytes = Buffer.from('\x89PNG\r\n\x1a\ne2e-sha-payload', 'binary');
    const wantSha = crypto.createHash('sha256').update(bytes).digest('hex').slice(0, 12);
    const srcPng = path.join(server.dir, 'shot.png');
    fs.writeFileSync(srcPng, bytes);

    const mcp = await mcpClient(server.url);

    // Seed an attachment into the live event bus via a non-blocking agent turn.
    await mcp.call('send_progress', { text: 'here is a screenshot', image_urls: [srcPng] });

    // Trigger the markdown export.
    const exp = await mcp.call('export_chat_md', { title: 'sha-suffix-check' });
    const summary = exp.content.map((c) => c.text).join('');
    const mdMatch = summary.match(/Exported chat to (\S+\.md)/);
    expect(mdMatch, `export summary should name the .md file; got: ${summary}`).toBeTruthy();
    const mdPath = mdMatch[1];

    // The asset must exist on disk with the content digest before the extension.
    const assetsDir = path.join(server.dir, 'agent-chats', 'assets');
    const assets = fs.readdirSync(assetsDir).filter((f) => f.endsWith('.png'));
    expect(assets.length, `expected one png asset, got ${JSON.stringify(assets)}`).toBe(1);
    const asset = assets[0];

    // Pattern: {YYYY-MM-DD}-{NN}-{N}-{sha12}.png
    expect(asset).toMatch(/^\d{4}-\d{2}-\d{2}-\d{2}-\d+-[0-9a-f]{12}\.png$/);
    // And the digest must be THIS file's content sha, not an arbitrary 12 hex.
    expect(asset).toContain(`-${wantSha}.png`);

    // No provisional/staging files left behind by the copy-then-rename step.
    expect(fs.readdirSync(assetsDir).some((f) => f.includes('.partial'))).toBe(false);

    // The exported markdown must link to the exact digest-suffixed filename.
    const md = fs.readFileSync(mdPath, 'utf8');
    expect(md).toContain(`./assets/${asset}`);

    // The copied bytes must be byte-identical to the source.
    const copied = fs.readFileSync(path.join(assetsDir, asset));
    expect(crypto.createHash('sha256').update(copied).digest('hex').slice(0, 12)).toBe(wantSha);
  });
});
