// MCP Apps host — shows a tool's interactive page inside the chat.
// Phase 4 of tasks/2026-11-17-mcp-apps-step1.md. Loaded after app.js and uses
// its globals (addBubble, submitUserMessage, scrollToBottom, SEND_SOURCES).
//
// An "mcpApp" event (posted by `agent-chat wrap`, see mcpapp.go) becomes an
// agent bubble holding an <iframe sandbox="allow-scripts">: no
// allow-same-origin, so the page runs on an opaque origin and can't read
// agent-chat's cookies, storage or DOM. Its html is fetched from
// api/mcp-app/page/<pageId> and loaded via srcdoc, behind a CSP built from the
// resource's _meta.ui.csp (or the spec's restrictive default).
//
// We speak the host side of the MCP Apps protocol (spec 2026-01-26):
// JSON-RPC 2.0 over postMessage, matched to its frame by event.source.
//   ui/initialize        → hostInfo, hostContext {theme}
//   ui/notifications/initialized → we send tool-input, then tool-result
//   ui/notifications/size-changed → iframe height (capped)
//   ui/message           → a user chat message "📱 From the … page: …",
//                          only while the user is in that page (it has focus)
//   tools/call           → run the tool through the wrapper that showed the
//                          page (step 2, POST api/mcp-app/call), after the
//                          user allows it; pages from an older wrapper get an
//                          error and fall back to ui/message
//   ui/open-link         → new tab, http(s) only
//   anything else        → -32601

'use strict';

var MCP_APP_PROTOCOL = '2026-01-26';
var MCP_APP_MIN_HEIGHT = 60;
var MCP_APP_MAX_HEIGHT = 800;
var MCP_APP_UI_MIME = 'text/html;profile=mcp-app';

// One entry per page on screen: { iframe, app, initialized, allowed, busy }.
var mcpAppFrames = [];

// Pages send chat messages as their own source: no interrupt word, no text
// box to lock (it wasn't typed into).
if (typeof SEND_SOURCES === 'object' && !SEND_SOURCES.page) {
  SEND_SOURCES.page = { mark: '', runOn: false, interrupts: false, locksInput: false };
}

function mcpAppName(app) {
  var name = (app.server ? app.server + '/' : '') + (app.tool || 'tool');
  return name.replace(/`/g, '');
}

function mcpAppTheme() {
  return document.documentElement.getAttribute('data-theme') === 'light' ? 'light' : 'dark';
}

// Only plain host sources may go into the CSP: no spaces, quotes or ';' that
// could add a directive of the page's own choosing.
function mcpAppCspSources(list) {
  if (!Array.isArray(list)) return [];
  return list.filter(function (s) {
    return typeof s === 'string' && /^[A-Za-z0-9.*:\/_-]+$/.test(s) && /^(https?|wss?):\/\//.test(s);
  });
}

function mcpAppCsp(resourceMeta) {
  var csp = (resourceMeta && resourceMeta.ui && resourceMeta.ui.csp) || {};
  var res = mcpAppCspSources(csp.resourceDomains);
  var conn = mcpAppCspSources(csp.connectDomains);
  var frames = mcpAppCspSources(csp.frameDomains);
  var base = mcpAppCspSources(csp.baseUriDomains);
  function d(name, fixed, extra) {
    var all = fixed.concat(extra);
    return name + ' ' + (all.length ? all.join(' ') : "'none'");
  }
  return [
    "default-src 'none'",
    d('script-src', ["'unsafe-inline'"], res),
    d('style-src', ["'unsafe-inline'"], res),
    d('img-src', ['data:', 'blob:'], res),
    d('font-src', ['data:'], res),
    d('media-src', ['data:', 'blob:'], res),
    d('connect-src', [], conn),
    d('frame-src', [], frames),
    d('base-uri', [], base),
    "form-action 'none'"
  ].join('; ');
}

// Put the CSP <meta> first thing in <head>, so it applies before any of the
// page's own scripts. A page can add a CSP of its own, but CSPs only ever
// tighten, never loosen.
function mcpAppWithCsp(html, resourceMeta) {
  var meta = '<meta http-equiv="Content-Security-Policy" content="' +
    mcpAppCsp(resourceMeta).replace(/"/g, '&quot;') + '">';
  var head = /<head(\s[^>]*)?>/i.exec(html);
  if (head) {
    var at = head.index + head[0].length;
    return html.slice(0, at) + meta + html.slice(at);
  }
  var doctype = /^\s*<!doctype[^>]*>/i.exec(html);
  if (doctype) {
    return doctype[0] + '<head>' + meta + '</head>' + html.slice(doctype[0].length);
  }
  return '<head>' + meta + '</head>' + html;
}

function mcpAppPost(entry, msg) {
  if (!entry.iframe.contentWindow) return;
  msg.jsonrpc = '2.0';
  // The page's origin is opaque ("null"), so '*' is the only target that
  // reaches it; contentWindow already pins the one frame we mean.
  entry.iframe.contentWindow.postMessage(msg, '*');
}

function mcpAppReply(entry, id, result) {
  mcpAppPost(entry, { id: id, result: result || {} });
}

function mcpAppError(entry, id, code, message) {
  mcpAppPost(entry, { id: id, error: { code: code, message: message } });
}

function mcpAppNotify(entry, method, params) {
  mcpAppPost(entry, { method: method, params: params || {} });
}

function mcpAppHostContext() {
  return { theme: mcpAppTheme(), displayMode: 'inline', availableDisplayModes: ['inline'] };
}

function mcpAppMessageText(content) {
  var parts = Array.isArray(content) ? content : [content];
  var texts = [];
  for (var i = 0; i < parts.length; i++) {
    var p = parts[i];
    if (p && p.type === 'text' && typeof p.text === 'string' && p.text.trim()) texts.push(p.text.trim());
  }
  return texts.join('\n\n');
}

function mcpAppHandle(entry, m) {
  var isRequest = m.id !== undefined && m.id !== null;
  var params = m.params || {};
  switch (m.method) {
    case 'ui/initialize':
      mcpAppReply(entry, m.id, {
        protocolVersion: MCP_APP_PROTOCOL,
        hostInfo: { name: 'agent-chat', version: (typeof SERVER_VERSION === 'string' ? SERVER_VERSION : '') },
        hostCapabilities: entry.app.wrapper ? { openLinks: {}, serverTools: {} } : { openLinks: {} },
        hostContext: mcpAppHostContext()
      });
      return;
    case 'ui/notifications/initialized':
      if (entry.initialized) return;
      entry.initialized = true;
      mcpAppNotify(entry, 'ui/notifications/tool-input', { arguments: entry.app.arguments || {} });
      if (entry.app.result) mcpAppNotify(entry, 'ui/notifications/tool-result', entry.app.result);
      return;
    case 'ui/notifications/size-changed':
      var h = Number(params.height);
      if (isFinite(h) && h > 0) {
        // The page reports its content height; the iframe's own border (it
        // is border-box here) comes on top, or the page gets a scrollbar.
        var chrome = entry.iframe.offsetHeight - entry.iframe.clientHeight;
        entry.iframe.style.height = (Math.max(MCP_APP_MIN_HEIGHT, Math.min(MCP_APP_MAX_HEIGHT, Math.ceil(h))) + chrome) + 'px';
        scrollToBottom(false);
      }
      return;
    case 'ui/message':
      if (!isRequest) return;
      // A page may only speak for the user while the user is in it: clicking
      // into the page gives its iframe focus. Stops a page from posting
      // messages on its own the moment it loads.
      if (document.activeElement !== entry.iframe) {
        mcpAppError(entry, m.id, -32000, 'ui/message needs the user to be using the page (click in it first)');
        return;
      }
      var text = params.role === 'user' || params.role === undefined ? mcpAppMessageText(params.content) : '';
      if (!text) {
        mcpAppError(entry, m.id, -32602, 'ui/message needs role "user" and text content');
        return;
      }
      submitUserMessage('📱 From the `' + mcpAppName(entry.app) + '` page: ' + text, 'page');
      mcpAppReply(entry, m.id, {});
      return;
    case 'tools/call':
      if (!isRequest) return;
      if (!entry.app.wrapper) {
        mcpAppError(entry, m.id, -32000, "this page's tool program can't be reached; use ui/message");
        return;
      }
      // Same rule as ui/message: only while the user is in the page.
      if (document.activeElement !== entry.iframe) {
        mcpAppError(entry, m.id, -32000, 'tools/call needs the user to be using the page (click in it first)');
        return;
      }
      if (entry.busy) {
        mcpAppError(entry, m.id, -32000, 'busy: one tool run at a time');
        return;
      }
      entry.busy = true;
      mcpAppAllowed(entry).then(function (ok) {
        if (!ok) {
          entry.busy = false;
          mcpAppError(entry, m.id, -32000, 'the user did not allow this page to run tools');
          return;
        }
        return mcpAppRunTool(entry, params).then(function (answer) {
          entry.busy = false;
          if (answer.error) mcpAppError(entry, m.id, answer.error.code || -32000, answer.error.message || 'tool failed');
          else mcpAppReply(entry, m.id, answer.result);
        });
      }).catch(function (err) {
        entry.busy = false;
        mcpAppError(entry, m.id, -32000, 'tool run failed: ' + (err && err.message));
      });
      return;
    case 'ui/open-link':
      if (!isRequest) return;
      var url = String(params.url || '');
      if (!/^https?:\/\//i.test(url)) {
        mcpAppError(entry, m.id, -32602, 'only http(s) links can be opened');
        return;
      }
      window.open(url, '_blank', 'noopener,noreferrer');
      mcpAppReply(entry, m.id, {});
      return;
    case 'ui/request-display-mode':
      if (isRequest) mcpAppReply(entry, m.id, { mode: 'inline' });
      return;
    case 'ping':
      if (isRequest) mcpAppReply(entry, m.id, {});
      return;
    case 'notifications/message':
      console.log('[mcp-app ' + mcpAppName(entry.app) + ']', params.level || 'info', params.data);
      return;
    default:
      if (isRequest && m.method) mcpAppError(entry, m.id, -32601, 'method not found: ' + m.method);
  }
}

/** Run a tool for a page through agent-chat and the wrapper that showed it.
    Resolves to {result} or {error}. */
function mcpAppRunTool(entry, params) {
  return fetch('api/mcp-app/call', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      wrapper: entry.app.wrapper,
      server: entry.app.server || '',
      tool: String(params.name || ''),
      arguments: params.arguments || {}
    })
  }).then(function (r) {
    return r.json().catch(function () { return { error: { code: -32000, message: 'HTTP ' + r.status } }; });
  });
}

/** Has the user allowed this page to run tools? Asks in the page's bubble
    the first time: Yes / No, plus Always allow after a Yes in an earlier
    session (remembered by agent-chat per tool program). Resolves true/false. */
function mcpAppAllowed(entry) {
  if (entry.allowed) return Promise.resolve(true);
  var server = entry.app.server || '';
  return fetch('api/mcp-app/allow?server=' + encodeURIComponent(server))
    .then(function (r) { return r.ok ? r.json() : {}; })
    .catch(function () { return {}; })
    .then(function (st) {
      if (st.always) { entry.allowed = true; return true; }
      return mcpAppAsk(entry, !!st.offerAlways).then(function (answer) {
        fetch('api/mcp-app/allow', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ server: server, answer: answer })
        }).catch(function () {});
        entry.allowed = answer !== 'no';
        return entry.allowed;
      });
    });
}

/** Show the question under the page; resolves 'yes', 'no' or 'always'. */
function mcpAppAsk(entry, offerAlways) {
  return new Promise(function (resolve) {
    var bubble = entry.iframe.closest('.bubble');
    var box = document.createElement('div');
    box.className = 'mcp-app-ask';
    box.setAttribute('role', 'group');
    var q = document.createElement('span');
    q.className = 'mcp-app-ask-text';
    q.textContent = entry.app.server
      ? 'Let the ' + entry.app.server + ' page run its tools?'
      : 'Let this page run its tools?';
    box.appendChild(q);
    function button(label, answer, cls) {
      var b = document.createElement('button');
      b.type = 'button';
      b.textContent = label;
      b.className = 'mcp-app-ask-btn' + (cls ? ' ' + cls : '');
      b.dataset.answer = answer;
      b.addEventListener('click', function () { box.remove(); resolve(answer); });
      box.appendChild(b);
    }
    button('Yes', 'yes', 'primary');
    if (offerAlways) button('Always allow', 'always');
    button('No', 'no');
    var menu = bubble && bubble.querySelector('.bubble-menu-btn');
    if (bubble) bubble.insertBefore(box, menu || null);
    scrollToBottom(false);
  });
}

// Settings: forget every "Always allow" and the earlier-session Yes answers.
(function () {
  var btn = document.getElementById('btn-mcp-app-forget');
  if (!btn) return;
  btn.addEventListener('click', function () {
    fetch('api/mcp-app/allow', { method: 'DELETE' }).then(function (r) {
      btn.textContent = r.ok ? 'Forgotten' : 'Could not forget (HTTP ' + r.status + ')';
      setTimeout(function () { btn.textContent = 'Forget allowed tool programs'; }, 2000);
    });
  });
})();

window.addEventListener('message', function (ev) {
  var m = ev.data;
  if (!m || typeof m !== 'object' || m.jsonrpc !== '2.0' || typeof m.method !== 'string') return;
  mcpAppFrames = mcpAppFrames.filter(function (e) { return e.iframe.isConnected; });
  for (var i = 0; i < mcpAppFrames.length; i++) {
    if (mcpAppFrames[i].iframe.contentWindow === ev.source) {
      mcpAppHandle(mcpAppFrames[i], m);
      return;
    }
  }
});

// Tell every page when the chat's theme flips.
new MutationObserver(function () {
  var ctx = mcpAppHostContext();
  mcpAppFrames.forEach(function (e) {
    e.iframe.style.colorScheme = ctx.theme;
    if (e.initialized && e.iframe.isConnected) mcpAppNotify(e, 'ui/notifications/host-context-changed', ctx);
  });
}).observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });

/** Draw an "mcpApp" event: an agent bubble with the tool's page in it. Used
    for live events and history replay alike, so a reload shows the same page
    with the same tool input and result. */
function renderMcpApp(event) {
  var app = event && event.app;
  if (!app || !app.pageId) return;
  var bubble = addBubble('📱 `' + mcpAppName(app) + '` page', 'agent', null, 'mcp-app', event.ts, undefined, event.seq, false);
  var ui = (app.resourceMeta && app.resourceMeta.ui) || {};
  var iframe = document.createElement('iframe');
  iframe.className = 'mcp-app-frame' + (ui.prefersBorder === false ? ' no-border' : '');
  iframe.setAttribute('sandbox', 'allow-scripts');
  iframe.setAttribute('referrerpolicy', 'no-referrer');
  iframe.setAttribute('title', mcpAppName(app) + ' page');
  iframe.dataset.appId = app.id || '';
  iframe.style.height = '150px';
  // Pages that follow the system colours (CSS Canvas/CanvasText) then match
  // the chat's theme; pages with fixed colours are unaffected.
  iframe.style.colorScheme = mcpAppTheme();
  // Before the menu button addBubble put last, so "⋯" stays at the end.
  var menu = bubble.querySelector('.bubble-menu-btn');
  bubble.insertBefore(iframe, menu);
  var entry = { iframe: iframe, app: app, initialized: false };
  mcpAppFrames.push(entry);

  fetch('api/mcp-app/page/' + encodeURIComponent(app.pageId))
    .then(function (r) {
      if (!r.ok) throw new Error('HTTP ' + r.status);
      return r.text();
    })
    .then(function (html) {
      iframe.srcdoc = mcpAppWithCsp(html, app.resourceMeta);
    })
    .catch(function (err) {
      iframe.remove();
      var note = document.createElement('div');
      note.className = 'mcp-app-error';
      note.textContent = 'Page could not be loaded (' + err.message + ').';
      bubble.insertBefore(note, menu);
    });
  scrollToBottom(false);
}
