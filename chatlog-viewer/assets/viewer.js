// Tiny markdown→bubbles renderer. Splits on `## Role` headings and renders
// each turn as a chat bubble. Uses marked.js (CDN) for the body markdown.

// Role markers we recognize. A turn starts on a line that is either a `## <role>`
// H2 heading OR a standalone `**<role>**` paragraph.
const ROLE_MAP = {
  user: 'user', you: 'user',
  agent: 'agent', claude: 'agent',
  system: 'system',
};
const TURN_RE = /^(?:## ([A-Za-z]+)|\*\*([A-Za-z]+)\*\*)\s*$/;

function unblockquote(s) {
  return s.split('\n').map(line => line.replace(/^> ?/, '')).join('\n');
}

function stripFrontmatter(md) {
  if (!md.startsWith('---\n')) return { meta: {}, body: md };
  const end = md.indexOf('\n---\n', 4);
  if (end < 0) return { meta: {}, body: md };
  const fm = md.slice(4, end);
  const body = md.slice(end + 5);
  const meta = {};
  for (const line of fm.split('\n')) {
    const m = line.match(/^([a-zA-Z0-9_-]+):\s*(.*)$/);
    if (m) meta[m[1]] = m[2];
  }
  return { meta, body };
}

// Table-format parser (Variant C / C′). Each turn is a 2-cell <table>; role is
// determined by which cell holds content (left-cell = agent, right-cell = user).
function splitTurnsFromTables(body) {
  const tableRE = /<table[^>]*>[\s\S]*?<\/table>/g;
  const tdRE = /<td[^>]*>([\s\S]*?)<\/td>/g;
  const isEmpty = s => {
    const t = s.replace(/&nbsp;/g, '').replace(/\s+/g, '');
    return t === '';
  };
  const turns = [];
  let lastEnd = 0;
  let preamble = '';
  let m;
  while ((m = tableRE.exec(body)) !== null) {
    const between = body.slice(lastEnd, m.index).trim();
    if (between) {
      // Text outside any table — treat as system/preamble. Append to preamble
      // if no turns yet, otherwise as a system turn.
      if (turns.length === 0) preamble += (preamble ? '\n\n' : '') + between;
      else turns.push({ role: 'system', body: between });
    }
    const tableInner = m[0];
    const cells = [];
    let cm;
    tdRE.lastIndex = 0;
    while ((cm = tdRE.exec(tableInner)) !== null) cells.push(cm[1]);
    if (cells.length === 2) {
      const [left, right] = cells.map(s => s.trim());
      let role, content;
      if (isEmpty(left) && !isEmpty(right))      { role = 'user';   content = right; }
      else if (isEmpty(right) && !isEmpty(left)) { role = 'agent';  content = left;  }
      else                                       { role = 'system'; content = left || right; }
      // Strip the leading **You** / **Claude** label (it's the in-bubble role
      // marker for the GitHub view; the bubble color already conveys this).
      content = content.replace(/^\s*\*\*(You|Claude|Agent|User|System)\*\*\s*\n*/, '');
      turns.push({ role, body: content.trim() });
    } else if (cells.length === 1) {
      // 1-cell table → centered system row.
      turns.push({ role: 'system', body: cells[0].trim() });
    }
    lastEnd = m.index + m[0].length;
  }
  // Trailing content after last table.
  const tail = body.slice(lastEnd).trim();
  if (tail) {
    if (turns.length === 0) preamble += (preamble ? '\n\n' : '') + tail;
    else turns.push({ role: 'system', body: tail });
  }
  return { preamble: preamble.trim(), turns };
}

// Heading / bold-line parser (Variants A, B, D). Splits on `## Role` or
// standalone `**Role**` lines. User-turn bodies have `> ` blockquote prefixes
// stripped.
function splitTurnsFromHeadings(body) {
  const lines = body.split('\n');
  const turns = [];
  let preamble = [];
  let current = null;
  for (const line of lines) {
    const m = line.match(TURN_RE);
    const rawRole = m && (m[1] || m[2]);
    const role = rawRole && ROLE_MAP[rawRole.toLowerCase()];
    if (role) {
      if (current) turns.push(current);
      current = { role, lines: [] };
    } else if (current) {
      current.lines.push(line);
    } else {
      preamble.push(line);
    }
  }
  if (current) turns.push(current);
  return {
    preamble: preamble.join('\n').trim(),
    turns: turns.map(t => {
      let bodyText = t.lines.join('\n').trim();
      if (t.role === 'user') bodyText = unblockquote(bodyText);
      return { role: t.role, body: bodyText };
    }),
  };
}

// Blockquote-prefix parser (Variants E, F). Lines like `> **You:** …` or
// `> **Claude:** …` start a new turn; bare content between them is the OTHER
// role. The "blockquote role" is whichever role appears first as `> **X:**`.
function splitTurnsFromPrefix(body) {
  const lines = body.split('\n');
  const PREFIX_RE = /^> \*\*(You|Claude|Agent|User):\*\*\s?(.*)$/;
  const QUOTE_CONT_RE = /^> ?(.*)$/;
  const turns = [];
  let preamble = [];
  let current = null;
  let blockquoteRole = null;

  const flush = () => { if (current) { current.body = current.body.trim(); turns.push(current); current = null; } };

  for (const line of lines) {
    const pm = line.match(PREFIX_RE);
    if (pm) {
      flush();
      const role = ROLE_MAP[pm[1].toLowerCase()];
      blockquoteRole = blockquoteRole || role;
      current = { role, body: pm[2] + '\n', source: 'quote' };
      continue;
    }
    if (current && current.source === 'quote') {
      const qm = line.match(QUOTE_CONT_RE);
      if (qm) { current.body += qm[1] + '\n'; continue; }
      flush();
    }
    // Bare line — belongs to the "other" role
    if (!current) {
      if (blockquoteRole === null) { preamble.push(line); continue; }
      const otherRole = blockquoteRole === 'user' ? 'agent' : 'user';
      current = { role: otherRole, body: '', source: 'bare' };
    }
    current.body += line + '\n';
  }
  flush();
  return {
    preamble: preamble.join('\n').trim(),
    turns: turns.map(t => ({ role: t.role, body: t.body.trim() })),
  };
}

function splitTurns(body) {
  // Auto-detect format and dispatch. Order matters: tables first (most specific),
  // then heading/bold markers, then blockquote-prefix.
  if (/<table[^>]*>[\s\S]*?<\/table>/.test(body)) {
    return splitTurnsFromTables(body);
  }
  if (TURN_RE.test(body) || body.split('\n').some(l => TURN_RE.test(l))) {
    return splitTurnsFromHeadings(body);
  }
  if (/^> \*\*(You|Claude|Agent|User):\*\*/m.test(body)) {
    return splitTurnsFromPrefix(body);
  }
  // Last resort — entire body as preamble.
  return { preamble: body.trim(), turns: [] };
}

async function loadChat(mdPath, container) {
  container = container || document.querySelector('.chat');
  container.innerHTML = '';
  try {
    const resp = await fetch(mdPath, { cache: 'no-cache' });
    if (!resp.ok) throw new Error('HTTP ' + resp.status);
    const md = await resp.text();
    const { meta, body } = stripFrontmatter(md);
    const { preamble, turns } = splitTurns(body);

    // Strip HTML comments (e.g. our `<!-- agent-chat export … -->` header) so
    // they don't create an empty system bubble at the top.
    const preambleClean = preamble.replace(/<!--[\s\S]*?-->/g, '').trim();
    if (preambleClean) {
      const pre = document.createElement('div');
      pre.className = 'bubble system';
      pre.innerHTML = marked.parse(preambleClean);
      container.appendChild(pre);
    }
    for (const turn of turns) {
      const bubble = document.createElement('div');
      bubble.className = 'bubble ' + turn.role;
      bubble.innerHTML = marked.parse(turn.body);
      container.appendChild(bubble);
    }
    return meta;
  } catch (e) {
    const err = document.createElement('div');
    err.className = 'error';
    err.textContent = 'Failed to load ' + mdPath + ': ' + e.message;
    container.appendChild(err);
    return {};
  }
}

// Legacy dropdown viewer (viewer.html). Looks for #chat-select; no-op if absent.
function init(manifest) {
  const select = document.querySelector('#chat-select');
  if (!select) return;
  for (const entry of manifest) {
    const opt = document.createElement('option');
    opt.value = entry.md;
    opt.textContent = entry.label;
    select.appendChild(opt);
  }
  const update = async () => {
    const meta = await loadChat(select.value);
    const tb = document.querySelector('.toolbar h1');
    if (tb && meta) {
      tb.textContent = (meta.date || '') + (meta.index ? '-' + meta.index : '') + ' · ' + (meta.title || select.value);
    }
    if (meta && meta.title) document.title = meta.title + ' — chat log';
  };
  select.addEventListener('change', update);
  if (manifest.length) update();
}

window.viewer = { init, load: loadChat };
