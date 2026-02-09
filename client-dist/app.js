// Agent Chat — browser client
// Plain JS, no build step. Connects to the Go server via WebSocket.

'use strict';

var messages = document.getElementById('messages');
var chatInput = document.getElementById('chat-input');
var sendBtn = document.getElementById('btn-send');
var statusDot = document.getElementById('status-dot');
var quickReplies = document.getElementById('quick-replies');

var pendingAckId = null;
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

function enableInput() {
  chatInput.disabled = false;
  sendBtn.disabled = false;
  quickReplies.classList.add('visible');
  chatInput.focus();
  setTimeout(function () { scrollToBottom(true); }, 100);
}

function disableInput() {
  chatInput.disabled = true;
  sendBtn.disabled = true;
  quickReplies.classList.remove('visible');
}

function showTyping() {
  sendBtn.classList.add('loading');
}

function hideTyping() {
  sendBtn.classList.remove('loading');
}

// --- Send ---

function sendAck(id, message) {
  if (activeWs && activeWs.readyState === WebSocket.OPEN) {
    var msg = { type: 'ack', id: id };
    if (message) {
      msg.message = message;
    }
    activeWs.send(JSON.stringify(msg));
  }
}

function handleSend() {
  if (!pendingAckId) return;

  var text = chatInput.value.trim();
  if (text) {
    addUserMessage(text);
  }
  isUserScrolledUp = false;
  sendAck(pendingAckId, text);
  pendingAckId = null;
  chatInput.value = '';
  disableInput();
  showTyping();
}

// Send on Enter or click
chatInput.addEventListener('keydown', function (e) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault();
    handleSend();
  }
});

sendBtn.addEventListener('click', handleSend);

// Quick-reply chips
quickReplies.addEventListener('click', function (e) {
  var chip = e.target.closest('.chip');
  if (!chip || chip.disabled || !pendingAckId) return;

  var message = chip.dataset.message || '';
  addUserMessage(message);
  isUserScrolledUp = false;
  sendAck(pendingAckId, message);
  pendingAckId = null;
  chatInput.value = '';
  disableInput();
  showTyping();
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

function replayHistory(history, reconnectAckId) {
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

  // If there's a pending ack, enable input so the user can respond
  if (reconnectAckId) {
    pendingAckId = reconnectAckId;
    hideTyping();
    enableInput();
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
          replayHistory(data.history, data.pendingAckId || null);
        }
        break;

      case 'agentMessage':
        console.log('[' + ts() + '] Agent message received: "' + data.text + '"');
        hideTyping();
        addAgentMessage(data.text || '');
        if (data.ack_id) {
          pendingAckId = data.ack_id;
          enableInput();
        }
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
