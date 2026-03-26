'use strict';

// ─── DOM references ────────────────────────────────────────────────────────

const statusDot = document.getElementById('statusDot');
const statusLabel = document.getElementById('statusLabel');
const tokenInput = document.getElementById('tokenInput');
const saveBtn = document.getElementById('saveBtn');
const saveMsg = document.getElementById('saveMsg');

// ─── Connection status ────────────────────────────────────────────────────

function setStatus(connected) {
  statusDot.className = 'status-dot ' + (connected ? 'connected' : 'disconnected');
  statusLabel.textContent = connected
    ? 'Connected to daemon'
    : 'Disconnected from daemon';
}

// Ask offscreen doc (via background) whether the WebSocket is live
function refreshStatus() {
  chrome.runtime.sendMessage({ type: 'ping-offscreen' }, (response) => {
    if (chrome.runtime.lastError) {
      setStatus(false);
      return;
    }
    setStatus(!!response?.connected);
  });
}

// Listen for live status updates pushed from background.js
chrome.runtime.onMessage.addListener((message) => {
  if (message.type === 'connection-status') {
    setStatus(message.connected);
  }
});

// ─── Token persistence ────────────────────────────────────────────────────

function loadToken() {
  chrome.storage.local.get('token', (result) => {
    if (result.token) {
      // Show a placeholder so the user knows a token is stored
      tokenInput.placeholder = '(token saved)';
    }
  });
}

function saveToken() {
  const token = tokenInput.value.trim();
  if (!token) {
    showSaveMsg('Please enter a token.', '#ea4335');
    return;
  }

  chrome.runtime.sendMessage({ type: 'set-token', token }, (response) => {
    if (chrome.runtime.lastError || !response?.ok) {
      showSaveMsg('Failed to save token.', '#ea4335');
      return;
    }
    tokenInput.value = '';
    tokenInput.placeholder = '(token saved)';
    showSaveMsg('Token saved!', '#34a853');
  });
}

function showSaveMsg(text, color) {
  saveMsg.textContent = text;
  saveMsg.style.color = color;
  setTimeout(() => {
    saveMsg.textContent = '';
  }, 3000);
}

// ─── Event listeners ──────────────────────────────────────────────────────

saveBtn.addEventListener('click', saveToken);

tokenInput.addEventListener('keydown', (event) => {
  if (event.key === 'Enter') saveToken();
});

// ─── Init ─────────────────────────────────────────────────────────────────

refreshStatus();
loadToken();
