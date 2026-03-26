/**
 * background.js - Service worker for chrome-pilot extension.
 *
 * Responsibilities:
 *  1. Ensure the Offscreen Document is always alive.
 *  2. Route commands received from the daemon to the appropriate Chrome API.
 *  3. Forward tab-change events to the daemon via the Offscreen Document.
 *  4. Token is auto-fetched by offscreen.js from daemon HTTP endpoint.
 */

'use strict';

const OFFSCREEN_URL = chrome.runtime.getURL('offscreen.html');
const OFFSCREEN_REASON = 'BLOBS'; // arbitrary permitted reason; keeps doc alive

// ─── Offscreen Document management ────────────────────────────────────────

async function ensureOffscreen() {
  // chrome.offscreen API: check if our document is already open
  const existingContexts = await chrome.runtime.getContexts({
    contextTypes: ['OFFSCREEN_DOCUMENT'],
    documentUrls: [OFFSCREEN_URL],
  }).catch(() => []);

  if (existingContexts.length > 0) return;

  await chrome.offscreen.createDocument({
    url: OFFSCREEN_URL,
    reasons: [chrome.offscreen.Reason.BLOBS],
    justification: 'Persistent WebSocket connection to chrome-pilot daemon',
  });
}

// Ping offscreen; if no reply, recreate it
async function checkOffscreenLiveness() {
  try {
    const reply = await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('timeout')), 3000);
      chrome.runtime.sendMessage({ type: 'ping-offscreen' }, (response) => {
        clearTimeout(timer);
        if (chrome.runtime.lastError) {
          reject(chrome.runtime.lastError);
        } else {
          resolve(response);
        }
      });
    });
    if (!reply?.alive) throw new Error('not alive');
  } catch (_err) {
    console.warn('[background] Offscreen unresponsive – recreating');
    try {
      await chrome.offscreen.closeDocument();
    } catch (_) { /* already gone */ }
    await ensureOffscreen();
  }
}

// ─── Alarm: keep offscreen alive ──────────────────────────────────────────

chrome.alarms.create('check-offscreen', { periodInMinutes: 0.5 }); // every 30s

chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === 'check-offscreen') {
    checkOffscreenLiveness();
  }
});

// ─── Send result/event back to daemon via offscreen ───────────────────────

function sendToDaemon(payload) {
  chrome.runtime.sendMessage({ type: 'send-to-daemon', payload }, (response) => {
    if (chrome.runtime.lastError) {
      console.warn('[background] sendToDaemon error:', chrome.runtime.lastError.message);
    }
  });
}

// ─── Content script helpers ───────────────────────────────────────────────

/**
 * Execute a single chrome.scripting.executeScript call that both
 * defines helpers AND runs the requested operation, so everything
 * shares a single scope.
 */
async function runInTab(tabId, method, params) {
  const results = await chrome.scripting.executeScript({
    target: { tabId },
    func: (method, params) => {
      // ── DOM helpers ──────────────────────────────────────────────────
      function querySelector(selector) {
        const el = document.querySelector(selector);
        if (!el) return null;
        return {
          tag: el.tagName.toLowerCase(),
          id: el.id || undefined,
          className: el.className || undefined,
          textContent: el.textContent?.trim().slice(0, 500),
          value: el.value ?? undefined,
          href: el.href ?? undefined,
        };
      }

      function querySelectorAll(selector) {
        return Array.from(document.querySelectorAll(selector)).map((el) => ({
          tag: el.tagName.toLowerCase(),
          id: el.id || undefined,
          className: el.className || undefined,
          textContent: el.textContent?.trim().slice(0, 200),
          value: el.value ?? undefined,
          href: el.href ?? undefined,
        }));
      }

      function clickElement(selector) {
        const el = document.querySelector(selector);
        if (!el) throw new Error(`Element not found: ${selector}`);
        el.click();
        return { clicked: selector };
      }

      function typeIntoElement(selector, text) {
        const el = document.querySelector(selector);
        if (!el) throw new Error(`Element not found: ${selector}`);
        el.focus();
        el.value = text;
        el.dispatchEvent(new Event('input', { bubbles: true }));
        el.dispatchEvent(new Event('change', { bubbles: true }));
        return { typed: text, into: selector };
      }

      function getSnapshot() {
        function nodeToObj(node, depth) {
          if (depth > 10) return null;
          if (node.nodeType === Node.TEXT_NODE) {
            const text = node.textContent?.trim();
            return text ? { type: 'text', content: text.slice(0, 200) } : null;
          }
          if (node.nodeType !== Node.ELEMENT_NODE) return null;
          const el = node;
          const children = Array.from(el.childNodes)
            .map((c) => nodeToObj(c, depth + 1))
            .filter(Boolean);
          return {
            tag: el.tagName.toLowerCase(),
            id: el.id || undefined,
            className: el.className || undefined,
            attrs: Array.from(el.attributes).reduce((acc, a) => {
              acc[a.name] = a.value;
              return acc;
            }, {}),
            children,
          };
        }
        return nodeToObj(document.body, 0);
      }

      // ── Dispatch ─────────────────────────────────────────────────────
      switch (method) {
        case 'dom.querySelector':
          return querySelector(params.selector);
        case 'dom.querySelectorAll':
          return querySelectorAll(params.selector);
        case 'dom.click':
          return clickElement(params.selector);
        case 'dom.type':
          return typeIntoElement(params.selector, params.text);
        case 'dom.evaluate': {
          // eslint-disable-next-line no-eval
          const fn = new Function('params', params.expression);
          return fn(params);
        }
        case 'snapshot':
        case 'page.snapshot':
          return getSnapshot();
        case 'page.title':
          return { title: document.title };
        case 'page.url':
          return { url: location.href };
        case 'page.scroll':
          window.scrollTo(params.x ?? 0, params.y ?? 0);
          return { scrolled: true };
        default:
          throw new Error(`Unknown content script method: ${method}`);
      }
    },
    args: [method, params],
  });

  if (!results || results.length === 0) {
    throw new Error('executeScript returned no results');
  }

  const first = results[0];
  if (first.error) throw first.error;
  return first.result;
}

// ─── Command dispatcher ────────────────────────────────────────────────────

async function handleCommand(command) {
  const { id, method, params = {} } = command;

  try {
    let result;

    if (method.startsWith('tab.')) {
      result = await handleTabCommand(method, params);
    } else if (method.startsWith('cookie.')) {
      result = await handleCookieCommand(method, params);
    } else if (method === 'page.screenshot') {
      result = await handleScreenshot(params);
    } else if (
      method.startsWith('dom.') ||
      method === 'snapshot' ||
      method.startsWith('page.')
    ) {
      const tabId = params.tabId ?? (await getActiveTabId());
      result = await runInTab(tabId, method, params);
    } else {
      throw new Error(`Unrecognised method: ${method}`);
    }

    sendToDaemon({ type: 'result', id, result });
  } catch (err) {
    sendToDaemon({ type: 'error', id, error: err.message ?? String(err) });
  }
}

async function getActiveTabId() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab) throw new Error('No active tab');
  return tab.id;
}

// ─── tab.* handlers ───────────────────────────────────────────────────────

async function handleTabCommand(method, params) {
  switch (method) {
    case 'tab.list':
      return chrome.tabs.query(params);
    case 'tab.get':
      return chrome.tabs.get(params.tabId);
    case 'tab.create':
      return chrome.tabs.create(params);
    case 'tab.update':
      return chrome.tabs.update(params.tabId, params.updateProperties ?? {});
    case 'tab.remove':
      await chrome.tabs.remove(params.tabId);
      return { removed: params.tabId };
    case 'tab.reload':
      await chrome.tabs.reload(params.tabId, params.reloadProperties);
      return { reloaded: params.tabId };
    case 'tab.activate': {
      const tab = await chrome.tabs.update(params.tabId, { active: true });
      return tab;
    }
    case 'tab.navigate':
      return chrome.tabs.update(params.tabId, { url: params.url });
    case 'tab.duplicate':
      return chrome.tabs.duplicate(params.tabId);
    case 'tab.query':
      return chrome.tabs.query(params);
    default:
      throw new Error(`Unknown tab method: ${method}`);
  }
}

// ─── cookie.* handlers ────────────────────────────────────────────────────

async function handleCookieCommand(method, params) {
  switch (method) {
    case 'cookie.get':
      return chrome.cookies.get(params);
    case 'cookie.getAll':
      return chrome.cookies.getAll(params);
    case 'cookie.set':
      return chrome.cookies.set(params);
    case 'cookie.remove':
      return chrome.cookies.remove(params);
    default:
      throw new Error(`Unknown cookie method: ${method}`);
  }
}

// ─── page.screenshot handler ──────────────────────────────────────────────

async function handleScreenshot(params) {
  const windowId = params.windowId ?? chrome.windows.WINDOW_ID_CURRENT;
  const options = { format: params.format ?? 'png' };
  if (params.quality !== undefined) options.quality = params.quality;

  const dataUrl = await chrome.tabs.captureVisibleTab(windowId, options);
  return { dataUrl };
}

// ─── Tab event monitoring ──────────────────────────────────────────────────

chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
  sendToDaemon({
    type: 'event',
    event: 'tab.updated',
    tabId,
    changeInfo,
    tab: {
      id: tab.id,
      url: tab.url,
      title: tab.title,
      status: tab.status,
      active: tab.active,
      windowId: tab.windowId,
    },
  });
});

chrome.tabs.onRemoved.addListener((tabId, removeInfo) => {
  sendToDaemon({
    type: 'event',
    event: 'tab.removed',
    tabId,
    removeInfo,
  });
});

chrome.tabs.onActivated.addListener((activeInfo) => {
  sendToDaemon({
    type: 'event',
    event: 'tab.activated',
    tabId: activeInfo.tabId,
    windowId: activeInfo.windowId,
  });
});

// ─── Message router ────────────────────────────────────────────────────────

chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  if (message.type === 'daemon-command') {
    handleCommand(message.payload);
    sendResponse({ ok: true });
    return false;
  }

  if (message.type === 'offscreen-status') {
    // Broadcast connection status to any open popups
    chrome.runtime.sendMessage({ type: 'connection-status', connected: message.connected }).catch(() => {});
    return false;
  }
});

// ─── Startup ──────────────────────────────────────────────────────────────

chrome.runtime.onInstalled.addListener(() => {
  ensureOffscreen();
});

chrome.runtime.onStartup.addListener(() => {
  ensureOffscreen();
});

// Also ensure on service worker activation
ensureOffscreen();
