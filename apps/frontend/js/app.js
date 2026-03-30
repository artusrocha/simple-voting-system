const API_BASE = '/api';

const APP_CONFIG = window.APP_CONFIG || {};

function getVoteMode() {
  const mode = String(APP_CONFIG.slideVoteMode || 'off').toLowerCase();
  if (mode === 'button' || mode === 'full') {
    return mode;
  }
  return 'off';
}

function isInteractionTelemetryEnabled() {
  return Boolean(APP_CONFIG.interactionTelemetry);
}

function isPowVoteEnabled() {
  return Boolean(APP_CONFIG.powVoteEnabled);
}

function getAssetVersion() {
  return String(APP_CONFIG.assetVersion || '').trim();
}

function withAssetVersion(path) {
  const version = getAssetVersion();
  if (!version) {
    return path;
  }
  const separator = path.includes('?') ? '&' : '?';
  return `${path}${separator}v=${encodeURIComponent(version)}`;
}

async function apiFetch(path, options = {}) {
  const url = path.startsWith('http') ? path : `${API_BASE}${path}`;
  const response = await fetch(url, {
    credentials: 'same-origin',
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options.headers,
    },
  });
  const rawBody = await response.text();
  let data;
  try {
    data = rawBody ? JSON.parse(rawBody) : {};
  } catch (_) {
    data = { message: rawBody || response.statusText || 'Request failed' };
  }
  return { ok: response.ok, status: response.status, data };
}

function prettyPrint(targetId, payload, ok) {
  const output = document.getElementById(targetId);
  if (!output) return;
  output.style.border = ok ? '1px solid #2f9e44' : '1px solid #c92a2a';
  output.textContent = JSON.stringify(payload, null, 2);
}

function showError(message) {
  const el = document.getElementById('error-message');
  if (el) {
    el.textContent = message;
    el.style.display = 'block';
  }
}

function hideError() {
  const el = document.getElementById('error-message');
  if (el) {
    el.style.display = 'none';
  }
}

function formatDate(isoString) {
  if (!isoString) return '';
  const date = new Date(isoString);
  return date.toLocaleDateString() + ' ' + date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function getRouter() {
  const routes = {
    '/vote': 'vote.html',
    '/admin': 'admin.html',
  };

  return {
    navigate(path) {
      window.location.href = path;
    },
    getRoute() {
      return window.location.pathname;
    },
  };
}
