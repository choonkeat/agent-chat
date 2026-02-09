// Agent Chat — browser client
// Plain JS, no build step. Connects to the Go server via WebSocket.

'use strict';

var messages = document.getElementById('messages');
var chatInput = document.getElementById('chat-input');
var sendBtn = document.getElementById('btn-send');
var statusDot = document.getElementById('status-dot');
var quickReplies = document.getElementById('quick-replies');

var activeWs = null;
var isUserScrolledUp = false;

// --- Scroll tracking ---

messages.addEventListener('scroll', function () {
  var threshold = 40;
  var distFromBottom = messages.scrollHeight - messages.scrollTop - messages.clientHeight;
  isUserScrolledUp = distFromBottom > threshold;
});

function scrollToBottom(force) {
  if (!force && isUserScrolledUp) return;
  messages.scrollTop = messages.scrollHeight;
}

// --- Timestamp helper ---

function ts() {
  return new Date().toISOString().slice(11, 23);
}

// --- Message rendering ---

function clearMessages() {
  messages.innerHTML = '';
}

function addBubble(text, role) {
  var div = document.createElement('div');
  div.className = 'bubble ' + role;
  div.textContent = text;
  messages.appendChild(div);
  scrollToBottom(false);
}

function addAgentMessage(text) {
  if (text) {
    addBubble(text, 'agent');
  }
}

function addUserMessage(text) {
  if (text) {
    addBubble(text, 'user');
  }
}

// --- Input enable/disable ---

function setQuickReplies(replies) {
  quickReplies.innerHTML = '';
  var items = replies && replies.length > 0 ? replies : ['Continue', 'Tell me more'];
  for (var i = 0; i < items.length; i++) {
    var btn = document.createElement('button');
    btn.className = 'chip';
    btn.dataset.message = items[i];
    btn.textContent = items[i];
    quickReplies.appendChild(btn);
  }
}

function enableInput(replies) {
  setQuickReplies(replies);
  chatInput.disabled = false;
  sendBtn.disabled = false;
  quickReplies.classList.add('visible');
  chatInput.focus();
  setTimeout(function () { scrollToBottom(true); }, 100);
}

function showLoading() {
  removeLoading();
  var div = document.createElement('div');
  div.className = 'bubble agent loading';
  div.id = 'loading-bubble';
  div.textContent = '...';
  messages.appendChild(div);
  scrollToBottom(false);
}

function removeLoading() {
  var el = document.getElementById('loading-bubble');
  if (el) el.remove();
}

// --- Send ---

function sendMessage(text) {
  if (activeWs && activeWs.readyState === WebSocket.OPEN) {
    activeWs.send(JSON.stringify({ type: 'message', text: text }));
  }
}

function handleSend() {
  var text = chatInput.value.trim();
  if (!text) return;

  addUserMessage(text);
  isUserScrolledUp = false;
  sendMessage(text);
  chatInput.value = '';
  chatInput.style.height = 'auto';
  showLoading();
}

// Auto-grow textarea
function autoGrow() {
  chatInput.style.height = 'auto';
  chatInput.style.height = Math.min(chatInput.scrollHeight, 150) + 'px';
  chatInput.style.overflowY = chatInput.scrollHeight > 150 ? 'auto' : 'hidden';
}

chatInput.addEventListener('input', autoGrow);

// Enter sends, Shift+Enter / Alt+Enter inserts newline
chatInput.addEventListener('keydown', function (e) {
  if (e.key === 'Enter' && !e.shiftKey && !e.altKey) {
    e.preventDefault();
    handleSend();
  }
});

sendBtn.addEventListener('click', handleSend);

// Quick-reply chips
quickReplies.addEventListener('click', function (e) {
  var chip = e.target.closest('.chip');
  if (!chip || chip.disabled) return;

  var message = chip.dataset.message || '';
  if (!message) return;
  addUserMessage(message);
  isUserScrolledUp = false;
  sendMessage(message);
  chatInput.value = '';
  showLoading();
});

// --- Connection status ---

function setStatus(state) {
  statusDot.className = state;
}

// --- WebSocket connection with exponential backoff ---

var BACKOFF_INITIAL = 1000;
var BACKOFF_MAX = 30000;
var backoffDelay = BACKOFF_INITIAL;
var reconnectTimer = null;

// --- History replay for browser reconnect ---

function replayHistory(history) {
  console.log('[' + ts() + '] Replaying ' + history.length + ' history events');
  clearMessages();

  for (var i = 0; i < history.length; i++) {
    var event = history[i];
    switch (event.type) {
      case 'agentMessage':
        if (event.text) {
          addBubble(event.text, 'agent');
        }
        break;
      case 'userMessage':
        if (event.text) {
          addBubble(event.text, 'user');
        }
        break;
    }
  }
}

// --- WebSocket connection ---

function teardown() {
  if (activeWs) {
    activeWs.onopen = null;
    activeWs.onmessage = null;
    activeWs.onclose = null;
    activeWs.onerror = null;
    activeWs.close();
    activeWs = null;
  }
  if (reconnectTimer !== null) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
}

function scheduleReconnect() {
  if (reconnectTimer !== null) return;
  reconnectTimer = setTimeout(function () {
    reconnectTimer = null;
    connect();
  }, backoffDelay);
  backoffDelay = Math.min(backoffDelay * 2, BACKOFF_MAX);
}

function connect() {
  teardown();
  setStatus('connecting');

  var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  var wsUrl = proto + '//' + location.host + '/ws';
  var ws = new WebSocket(wsUrl);
  activeWs = ws;

  ws.onopen = function () {
    console.log('[' + ts() + '] WebSocket onopen');
    setStatus('connected');
    backoffDelay = BACKOFF_INITIAL;
  };

  ws.onmessage = function (event) {
    if (ws !== activeWs) return;
    var data = JSON.parse(event.data);

    switch (data.type) {
      case 'connected':
        console.log('[' + ts() + '] Connected event received');
        setStatus('connected');
        if (data.history && Array.isArray(data.history) && data.history.length > 0) {
          replayHistory(data.history);
        }
        enableInput();
        break;

      case 'agentMessage':
        console.log('[' + ts() + '] Agent message received: "' + data.text + '"');
        removeLoading();
        addAgentMessage(data.text || '');
        enableInput(data.quick_replies);
        break;
    }
  };

  ws.onclose = function () {
    if (ws !== activeWs) return;
    console.log('[' + ts() + '] WebSocket closed, reconnecting...');
    teardown();
    setStatus('connecting');
    scheduleReconnect();
  };

  ws.onerror = function () {
    if (ws !== activeWs) return;
    console.log('[' + ts() + '] WebSocket error');
  };
}

connect();
