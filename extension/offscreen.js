/**
 * offscreen.js - Persistent WebSocket client for chrome-pilot daemon.
 *
 * Runs in an Offscreen Document so it survives service worker suspension.
 * Connects to ws://localhost:9333/ws, authenticates with a token fetched
 * from the background service worker, and bridges messages bidirectionally:
 *   daemon  →  background.js  (commands)
 *   background.js  →  daemon  (events / command results)
 */

'use strict';

const WS_URL = 'ws://localhost:9333/ws';
const HEARTBEAT_INTERVAL_MS = 20_000;
const RECONNECT_BASE_MS = 1_000;
const RECONNECT_MAX_MS = 30_000;

let ws = null;
let reconnectDelay = RECONNECT_BASE_MS;
let heartbeatTimer = null;
let isConnected = false;

// ─── Token retrieval ───────────────────────────────────────────────────────

function getToken() {
  return new Promise((resolve) => {
    chrome.runtime.sendMessage({ type: 'get-token' }, (response) => {
      if (chrome.runtime.lastError) {
        console.warn('[offscreen] get-token error:', chrome.runtime.lastError.message);
        resolve(null);
      } else {
        resolve(response?.token ?? null);
      }
    });
  });
}

// ─── Heartbeat ─────────────────────────────────────────────────────────────

function startHeartbeat() {
  stopHeartbeat();
  heartbeatTimer = setInterval(() => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'ping' }));
    }
  }, HEARTBEAT_INTERVAL_MS);
}

function stopHeartbeat() {
  if (heartbeatTimer !== null) {
    clearInterval(heartbeatTimer);
    heartbeatTimer = null;
  }
}

// ─── WebSocket lifecycle ───────────────────────────────────────────────────

async function connect() {
  const token = await getToken();

  ws = new WebSocket(WS_URL);

  ws.addEventListener('open', () => {
    console.log('[offscreen] WebSocket connected');
    isConnected = true;
    reconnectDelay = RECONNECT_BASE_MS;

    // Authenticate immediately after connection
    ws.send(JSON.stringify({ type: 'auth', token: token || '' }));
    startHeartbeat();

    // Notify background that we are online
    chrome.runtime.sendMessage({ type: 'offscreen-status', connected: true });
  });

  ws.addEventListener('message', (event) => {
    let msg;
    try {
      msg = JSON.parse(event.data);
    } catch (err) {
      console.warn('[offscreen] Unparseable message:', event.data);
      return;
    }

    if (msg.type === 'auth-response') {
      if (!msg.ok) {
        console.error('[offscreen] Auth rejected by daemon');
      } else {
        console.log('[offscreen] Auth accepted');
      }
      return;
    }

    if (msg.type === 'pong') {
      // Heartbeat acknowledged – nothing to do
      return;
    }

    // All other messages are commands; forward to background.js
    chrome.runtime.sendMessage({ type: 'daemon-command', payload: msg }, (response) => {
      if (chrome.runtime.lastError) {
        console.warn('[offscreen] sendMessage error:', chrome.runtime.lastError.message);
      }
    });
  });

  ws.addEventListener('close', (event) => {
    console.warn(`[offscreen] WebSocket closed (code=${event.code}). Reconnecting in ${reconnectDelay}ms`);
    isConnected = false;
    stopHeartbeat();
    chrome.runtime.sendMessage({ type: 'offscreen-status', connected: false });
    scheduleReconnect();
  });

  ws.addEventListener('error', (event) => {
    console.error('[offscreen] WebSocket error', event);
    // The 'close' event will fire afterwards and trigger reconnect
  });
}

function scheduleReconnect() {
  setTimeout(() => {
    connect();
  }, reconnectDelay);

  // Exponential backoff: double delay, cap at max
  reconnectDelay = Math.min(reconnectDelay * 2, RECONNECT_MAX_MS);
}

// ─── Messages from background.js ──────────────────────────────────────────

chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  if (message.type === 'send-to-daemon') {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(message.payload));
      sendResponse({ ok: true });
    } else {
      console.warn('[offscreen] Cannot send – WebSocket not open');
      sendResponse({ ok: false, error: 'not connected' });
    }
    return true; // Keep channel open for async sendResponse
  }

  if (message.type === 'ping-offscreen') {
    sendResponse({ alive: true, connected: isConnected });
    return true;
  }
});

// ─── Bootstrap ────────────────────────────────────────────────────────────

connect();
