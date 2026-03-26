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

// ─── Token retrieval (auto-fetch from daemon HTTP endpoint) ──────────────

async function getToken() {
  try {
    const resp = await fetch('http://localhost:9333/token');
    if (resp.ok) return await resp.text();
  } catch (_) { /* daemon not running yet */ }
  return null;
}

// ─── Heartbeat ─────────────────────────────────────────────────────────────

function startHeartbeat() {
  stopHeartbeat();
  heartbeatTimer = setInterval(() => {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ method: 'ping' }));
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

    // Authenticate immediately after connection (format must match wsserver.go)
    ws.send(JSON.stringify({ method: 'auth', params: { token: token || '' } }));
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

    // Auth response from wsserver.go: {method: "auth", result: {ok: true}}
    if (msg.method === 'auth') {
      if (msg.result?.ok) {
        console.log('[offscreen] Auth accepted');
      } else {
        console.error('[offscreen] Auth rejected by daemon:', msg.error);
      }
      return;
    }

    if (msg.method === 'pong' || msg.type === 'pong') {
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

  if (message.type === 'send-event') {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({
        id: null,
        event: message.event,
        data: message.data,
      }));
      sendResponse({ ok: true });
    } else {
      console.warn('[offscreen] Cannot send event – WebSocket not open');
      sendResponse({ ok: false, error: 'not connected' });
    }
    return true;
  }
});

// ─── Bootstrap ────────────────────────────────────────────────────────────

connect();
