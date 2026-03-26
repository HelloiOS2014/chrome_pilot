'use strict';

const statusDot = document.getElementById('statusDot');
const statusLabel = document.getElementById('statusLabel');

function setStatus(connected) {
  statusDot.className = 'status-dot ' + (connected ? 'connected' : 'disconnected');
  statusLabel.textContent = connected
    ? 'Connected to daemon'
    : 'Disconnected from daemon';
}

function refreshStatus() {
  chrome.runtime.sendMessage({ type: 'ping-offscreen' }, (response) => {
    if (chrome.runtime.lastError) {
      setStatus(false);
      return;
    }
    setStatus(!!response?.connected);
  });
}

chrome.runtime.onMessage.addListener((message) => {
  if (message.type === 'connection-status') {
    setStatus(message.connected);
  }
});

refreshStatus();
