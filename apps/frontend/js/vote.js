let votings = [];
let selectedVoting = null;
let selectedCandidate = null;
let activeCandidateId = null;
let voteFlowLocked = false;
let slideVoteControllers = [];
let interactionSignals = {};
let powWorker = null;

const POST_CONFIRM_DELAY_MS = Number(APP_CONFIG.postConfirmDelayMs || 500);
const RESULTS_RETRY_ATTEMPTS = Number(APP_CONFIG.resultsRetryAttempts || 3);
const RESULTS_RETRY_INTERVAL_MS = Number(APP_CONFIG.resultsRetryIntervalMs || 200);
const SLIDE_COMPLETE_THRESHOLD = 0.92;

function getPowWorker() {
  if (!powWorker) {
    powWorker = new Worker(withAssetVersion('/js/pow-worker.js'));
  }
  return powWorker;
}

async function requestVoteChallenge(votingId) {
  const { ok, data } = await apiFetch(`/votings/${votingId}/vote-challenges`, {
    method: 'POST',
    body: JSON.stringify({}),
  });

  if (!ok) {
    throw new Error(data?.message || 'Failed to prepare anti-bot challenge');
  }

  return data;
}

function solvePowChallenge(challenge) {
  return new Promise((resolve, reject) => {
    const worker = getPowWorker();
    const cleanup = () => {
      worker.removeEventListener('message', handleMessage);
      worker.removeEventListener('error', handleError);
    };
    const handleMessage = event => {
      cleanup();
      const { success, nonce, error } = event.data || {};
      if (!success) {
        reject(new Error(error || 'Failed to solve anti-bot challenge'));
        return;
      }
      resolve(nonce);
    };
    const handleError = error => {
      cleanup();
      reject(error instanceof Error ? error : new Error('Worker failure'));
    };
    worker.addEventListener('message', handleMessage);
    worker.addEventListener('error', handleError);
    worker.postMessage({
      assetVersion: getAssetVersion(),
      challengeId: challenge.challengeId,
      token: challenge.token,
      algorithm: challenge.algorithm,
      difficultyBits: challenge.difficultyBits,
      params: challenge.params,
    });
  });
}

async function createPowProof(votingId, statusEl) {
  let attempts = 0;
  while (attempts < 2) {
    attempts += 1;
    statusEl.className = 'vote-status pending';
    statusEl.textContent = attempts === 1 ? 'Preparing challenge...' : 'Retrying expired challenge...';
    const challenge = await requestVoteChallenge(votingId);
    const challengeReceivedAt = new Date();
    statusEl.textContent = 'Computing anti-bot proof...';
    const solveStartedAt = new Date();
    const nonce = await solvePowChallenge(challenge);
    const solveCompletedAt = new Date();
    if (!challenge.expiresAt || Date.parse(challenge.expiresAt) > Date.now()) {
      return {
        challenge,
        nonce,
        metrics: {
          challengeId: challenge.challengeId,
          algorithm: challenge.algorithm,
          challengeReceivedAt: challengeReceivedAt.toISOString(),
          solveStartedAt: solveStartedAt.toISOString(),
          solveCompletedAt: solveCompletedAt.toISOString(),
          solveDurationMs: Math.max(solveCompletedAt.getTime() - solveStartedAt.getTime(), 0),
          retryAttempt: attempts - 1,
        },
      };
    }
  }
  throw new Error('Anti-bot challenge expired before completion');
}

function getClientContext() {
  const nav = window.navigator || {};
  const screenInfo = window.screen || {};
  return {
    userAgent: String(nav.userAgent || ''),
    platform: String(nav.platform || ''),
    language: String(nav.language || ''),
    languages: Array.isArray(nav.languages) ? nav.languages.map(value => String(value)) : [],
    screenWidth: Number(screenInfo.width || 0),
    screenHeight: Number(screenInfo.height || 0),
    viewportWidth: Number(window.innerWidth || 0),
    viewportHeight: Number(window.innerHeight || 0),
    devicePixelRatio: Number(window.devicePixelRatio || 1),
    maxTouchPoints: Number(nav.maxTouchPoints || 0),
    timezone: (() => {
      try {
        return Intl.DateTimeFormat().resolvedOptions().timeZone || '';
      } catch (_) {
        return '';
      }
    })(),
    mobile: /Android|iPhone|iPad|iPod|Mobile/i.test(String(nav.userAgent || '')),
  };
}

function getVotingAntiAbuse() {
  if (!selectedVoting) {
    return {
      honeypotEnabled: false,
      slideVoteMode: 'off',
      interactionTelemetryEnabled: false,
      pow: {
        enabled: false,
        algorithm: 'sha256',
        ttlSeconds: 60,
        baseDifficultyBits: 18,
        maxDifficultyBits: 24,
        adaptiveWindowSeconds: 60,
        memoryKiB: 8192,
        timeCost: 1,
        parallelism: 1,
        hashLength: 32,
      },
    };
  }
  const antiAbuse = selectedVoting?.antiAbuse || {};
  const pow = antiAbuse.pow || {};
  return {
    honeypotEnabled: antiAbuse.honeypotEnabled !== false,
    slideVoteMode: String(antiAbuse.slideVoteMode || 'off').toLowerCase(),
    interactionTelemetryEnabled: Boolean(antiAbuse.interactionTelemetryEnabled),
    pow: {
      enabled: Boolean(pow.enabled),
      algorithm: String(pow.algorithm || 'sha256').toLowerCase(),
      ttlSeconds: Number(pow.ttlSeconds || 60),
      baseDifficultyBits: Number(pow.baseDifficultyBits || 18),
      maxDifficultyBits: Number(pow.maxDifficultyBits || 24),
      adaptiveWindowSeconds: Number(pow.adaptiveWindowSeconds || 60),
      memoryKiB: Number(pow.memoryKiB || 8192),
      timeCost: Number(pow.timeCost || 1),
      parallelism: Number(pow.parallelism || 1),
      hashLength: Number(pow.hashLength || 32),
    },
  };
}

function getVotingSlideMode() {
  const desiredMode = getVotingAntiAbuse().slideVoteMode;
  const capabilityMode = getVoteMode();
  if (capabilityMode === 'off') {
    return 'off';
  }
  if (capabilityMode === 'button' && desiredMode === 'full') {
    return 'button';
  }
  if (desiredMode === 'button' || desiredMode === 'full') {
    return desiredMode;
  }
  return 'off';
}

function isVotingInteractionTelemetryEnabled() {
  return isInteractionTelemetryEnabled() && getVotingAntiAbuse().interactionTelemetryEnabled;
}

function isVotingPowEnabled() {
  return isPowVoteEnabled() && getVotingAntiAbuse().pow.enabled;
}

function isVotingHoneypotEnabled() {
  return getVotingAntiAbuse().honeypotEnabled;
}

function renderHoneypotField() {
  const container = document.getElementById('honeypot-container');
  if (!container) return;
  if (!isVotingHoneypotEnabled()) {
    container.innerHTML = '';
    return;
  }
  container.innerHTML = `
    <label class="honeypot-field" aria-hidden="true">
      <span>Leave this field empty</span>
      <input id="vote-honeypot" type="text" name="website" autocomplete="off" tabindex="-1" />
    </label>
  `;
}

function getHoneypotValue() {
  const input = document.getElementById('vote-honeypot');
  return input ? input.value : '';
}

function getVoteModeNote() {
  const mode = getVotingSlideMode();
  if (mode === 'full') {
    return 'Tap a candidate card to open its slider and confirm the vote with one gesture.';
  }
  if (mode === 'button') {
    return 'Select a candidate, then slide to confirm your vote.';
  }
  return 'Select a candidate and submit your vote.';
}

function getInteractionSignal(candidateId) {
  const signal = interactionSignals[candidateId] || {};
  if (!isVotingInteractionTelemetryEnabled()) {
    return undefined;
  }

  return {
    openedAt: signal.openedAt || null,
    startedAt: signal.startedAt || null,
    completedAt: signal.completedAt || null,
    openToStartMs: signal.openToStartMs || 0,
    gestureDurationMs: signal.gestureDurationMs || 0,
    moveEvents: signal.moveEvents || 0,
    maxProgress: Number(signal.maxProgress || 0),
    completed: Boolean(signal.completed),
    cancelled: Boolean(signal.cancelled),
    mode: getVotingSlideMode(),
  };
}

function ensureInteractionSignal(candidateId) {
  if (!interactionSignals[candidateId]) {
    interactionSignals[candidateId] = {
      openedAt: new Date().toISOString(),
      moveEvents: 0,
      maxProgress: 0,
      completed: false,
      cancelled: false,
    };
  }
  return interactionSignals[candidateId];
}

async function loadVotings() {
  const listEl = document.getElementById('voting-list');
  listEl.innerHTML = '<p>Loading votings...</p>';

  const { ok, data } = await apiFetch('/votings?status=OPEN');
  if (!ok) {
    listEl.innerHTML = '<p class="error">Failed to load votings</p>';
    return;
  }

  votings = Array.isArray(data) ? data : [];

  if (votings.length === 0) {
    listEl.innerHTML = '<p>No open votings available.</p>';
    return;
  }

  renderVotingList();
}

function renderVotingList() {
  const listEl = document.getElementById('voting-list');
  listEl.innerHTML = votings.map(v => `
    <div class="voting-card" onclick="selectVoting('${v.votingId}')">
      <h3>${escapeHtml(v.name)}</h3>
      <div class="meta">
        <span class="status-badge status-${v.status}">${v.status}</span>
        ${v.candidates ? ` · ${v.candidates.length} candidates` : ''}
        ${v.endsAt ? ` · Ends: ${formatDate(v.endsAt)}` : ''}
      </div>
    </div>
  `).join('');
}

function resetVoteState() {
  selectedCandidate = null;
  activeCandidateId = null;
  voteFlowLocked = false;
  interactionSignals = {};
  teardownSlideVotes();
}

async function selectVoting(votingId) {
  const listItem = votings.find(v => v.votingId === votingId);
  if (!listItem) return;

  const { ok, data } = await apiFetch(`/votings/${votingId}`);
  selectedVoting = ok && data ? data : listItem;

  resetVoteState();
  document.getElementById('voting-list-section').style.display = 'none';
  document.getElementById('vote-form-section').style.display = 'block';
  document.getElementById('voting-name').textContent = selectedVoting.name;
  document.getElementById('vote-status').textContent = '';
  document.getElementById('vote-next-step').style.display = 'none';
  hideResultsPanorama();
  renderHoneypotField();
  renderCandidates();
  renderVoteActionSlot();
}

function isFullMode() {
	return getVotingSlideMode() === 'full';
}

function isButtonMode() {
	return getVotingSlideMode() === 'button';
}

function renderCandidates() {
  teardownSlideVotes();

  const container = document.getElementById('candidate-list');
  const candidates = selectedVoting?.candidates || [];
  const fullMode = isFullMode();

  container.innerHTML = candidates.map(candidate => {
    const candidateId = escapeHtml(candidate.candidateId);
    const selectedClass = selectedCandidate === candidate.candidateId ? 'selected' : '';
    const activeClass = activeCandidateId === candidate.candidateId ? 'active' : '';
    const lockedClass = voteFlowLocked ? 'locked' : '';
    const radioMarkup = fullMode ? '' : `
      <input type="radio" name="candidate" value="${candidateId}" ${selectedCandidate === candidate.candidateId ? 'checked' : ''}>
    `;
    const slideShell = fullMode ? `
      <div class="candidate-slide-shell ${activeCandidateId === candidate.candidateId ? 'visible' : ''}" id="candidate-slide-shell-${candidateId}">
        ${activeCandidateId === candidate.candidateId ? buildSlideVoteMarkup(`candidate-${candidateId}`, `Slide to vote for ${escapeHtml(candidate.name)}`) : ''}
      </div>
    ` : '';

    return `
      <article class="candidate-option ${selectedClass} ${activeClass} ${lockedClass}" onclick="activateCandidateCard('${candidateId}')">
        <div class="candidate-option-head">
          ${radioMarkup}
          <div class="candidate-option-body">
            <span class="candidate-option-title">${escapeHtml(candidate.name)}</span>
            <span class="candidate-option-meta">${candidateId}</span>
          </div>
        </div>
        ${slideShell}
      </article>
    `;
  }).join('');

  if (!fullMode) {
    document.querySelectorAll('input[name="candidate"]').forEach(input => {
      input.addEventListener('click', event => event.stopPropagation());
      input.addEventListener('change', event => {
        activateCandidateCard(event.target.value);
      });
    });
  }

  if (fullMode && activeCandidateId) {
    mountCandidateSlider(activeCandidateId);
  }
}

function renderVoteActionSlot() {
  const slot = document.getElementById('vote-action-slot');
  const note = `<p class="vote-mode-note">${escapeHtml(getVoteModeNote())}</p>`;

  if (isFullMode()) {
    slot.innerHTML = note;
    return;
  }

  if (isButtonMode()) {
    slot.innerHTML = note + (selectedCandidate
      ? buildSlideVoteMarkup('global-submit', 'Slide to confirm your vote')
      : '<p class="vote-mode-note">Choose a candidate to unlock the confirmation slider.</p>');
    if (selectedCandidate) {
      mountGlobalSlider();
    }
    return;
  }

  slot.innerHTML = `${note}<button id="vote-btn" class="submit-btn" onclick="submitVote()">Vote</button>`;
}

function buildSlideVoteMarkup(idPrefix, label) {
  return `
    <div class="slide-vote" id="${idPrefix}-wrapper">
      <span class="slide-vote-label">${escapeHtml(label)}</span>
      <div class="slide-vote-track" id="${idPrefix}-track" data-label="Slide to vote" data-complete-label="Processing...">
        <div class="slide-vote-progress" id="${idPrefix}-progress"></div>
        <button type="button" class="slide-vote-thumb" id="${idPrefix}-thumb" aria-label="${escapeHtml(label)}"></button>
      </div>
    </div>
  `;
}

function activateCandidateCard(candidateId) {
  if (voteFlowLocked || !selectedVoting) {
    return;
  }

  selectedCandidate = candidateId;
  ensureInteractionSignal(candidateId).openedAt = new Date().toISOString();

  if (isFullMode()) {
    activeCandidateId = candidateId;
  }

  renderCandidates();
  renderVoteActionSlot();
}

function teardownSlideVotes() {
  slideVoteControllers.forEach(cleanup => cleanup());
  slideVoteControllers = [];
}

function mountCandidateSlider(candidateId) {
  const candidate = (selectedVoting?.candidates || []).find(item => item.candidateId === candidateId);
  if (!candidate) return;

  mountSlideVote(`candidate-${candidateId}`, candidateId, {
    label: `Slide to vote for ${candidate.name}`,
    onComplete: async signal => {
      await submitVote(candidateId, signal);
    },
  });
}

function mountGlobalSlider() {
  const candidate = (selectedVoting?.candidates || []).find(item => item.candidateId === selectedCandidate);
  if (!candidate) return;

  mountSlideVote('global-submit', selectedCandidate, {
    label: `Slide to confirm vote for ${candidate.name}`,
    onComplete: async signal => {
      await submitVote(selectedCandidate, signal);
    },
  });
}

function mountSlideVote(idPrefix, candidateId, options) {
  const track = document.getElementById(`${idPrefix}-track`);
  const progress = document.getElementById(`${idPrefix}-progress`);
  const thumb = document.getElementById(`${idPrefix}-thumb`);
  if (!track || !progress || !thumb) {
    return;
  }

  const signal = ensureInteractionSignal(candidateId);
  let dragging = false;
  let maxX = 0;
  let thumbX = 0;
  let startPointerX = 0;
  let completed = false;

  const sync = x => {
    const width = track.clientWidth;
    const thumbWidth = thumb.offsetWidth;
    maxX = Math.max(width - thumbWidth - 12, 0);
    thumbX = Math.max(0, Math.min(x, maxX));
    thumb.style.transform = `translateX(${thumbX}px)`;
    progress.style.width = `${thumbX + thumbWidth}px`;
    const ratio = maxX === 0 ? 0 : thumbX / maxX;
    signal.maxProgress = Math.max(signal.maxProgress || 0, ratio);
  };

  const reset = cancelled => {
    dragging = false;
    if (cancelled) {
      signal.cancelled = true;
    }
    thumb.style.transition = 'transform 0.2s ease';
    progress.style.transition = 'width 0.2s ease';
    sync(0);
    window.setTimeout(() => {
      thumb.style.transition = '';
      progress.style.transition = '';
    }, 220);
  };

  const onMove = event => {
    if (!dragging || completed) return;
    signal.moveEvents = (signal.moveEvents || 0) + 1;
    sync(event.clientX - startPointerX);
  };

  const finish = async success => {
    if (!dragging || completed) return;
    dragging = false;
    document.removeEventListener('pointermove', onMove);
    document.removeEventListener('pointerup', onUp);

    const ratio = maxX === 0 ? 0 : thumbX / maxX;
    if (!success && ratio < SLIDE_COMPLETE_THRESHOLD) {
      reset(true);
      return;
    }

    completed = true;
    signal.completed = true;
    signal.completedAt = new Date().toISOString();
    signal.gestureDurationMs = Date.now() - Date.parse(signal.startedAt || signal.completedAt);
    thumb.disabled = true;
    sync(maxX);
    track.classList.add('completed');
    await options.onComplete(getInteractionSignal(candidateId));
  };

  const onUp = () => {
    finish(false);
  };

  const onDown = event => {
    if (voteFlowLocked || completed) return;
    event.preventDefault();
    signal.startedAt = new Date().toISOString();
    signal.openToStartMs = signal.openedAt ? Date.now() - Date.parse(signal.openedAt) : 0;
    startPointerX = event.clientX - thumbX;
    dragging = true;
    document.addEventListener('pointermove', onMove);
    document.addEventListener('pointerup', onUp, { once: true });
  };

  thumb.addEventListener('pointerdown', onDown);
  track.addEventListener('dblclick', event => event.preventDefault());
  sync(0);

  slideVoteControllers.push(() => {
    document.removeEventListener('pointermove', onMove);
    document.removeEventListener('pointerup', onUp);
    thumb.removeEventListener('pointerdown', onDown);
  });
}

function hideResultsPanorama() {
  const panoramaEl = document.getElementById('results-panorama');
  panoramaEl.style.display = 'none';
  document.getElementById('results-summary').innerHTML = '';
  document.getElementById('results-breakdown').innerHTML = '';
}

function renderResultsPanorama(results) {
  const panoramaEl = document.getElementById('results-panorama');
  const summaryEl = document.getElementById('results-summary');
  const breakdownEl = document.getElementById('results-breakdown');
  const candidates = selectedVoting?.candidates || [];
  const totalVotes = results.totalVotes || 0;
  const updatedAt = results.updatedAt ? formatDate(results.updatedAt) : 'Not available';

  summaryEl.innerHTML = `
    <div class="results-summary-chip">
      <span>Total votes</span>
      <strong>${totalVotes}</strong>
    </div>
    <div class="results-summary-chip">
      <span>Updated at</span>
      <strong>${escapeHtml(updatedAt)}</strong>
    </div>
  `;

  breakdownEl.innerHTML = candidates.map(candidate => {
    const count = results.byCandidate?.[candidate.candidateId] || 0;
    const percentage = Number(results.percentageByCandidate?.[candidate.candidateId] || 0);
    const isSelected = candidate.candidateId === selectedCandidate;

    return `
      <article class="results-row ${isSelected ? 'selected' : ''}">
        <div class="results-row-head">
          <div>
            <strong>${escapeHtml(candidate.name)}</strong>
            <span>${escapeHtml(candidate.candidateId)}</span>
          </div>
          <span>${isSelected ? 'Your vote' : 'Current partial result'}</span>
        </div>
        <div class="results-row-stats">
          <strong>${percentage.toFixed(1)}%</strong>
          <span>${count} vote${count === 1 ? '' : 's'}</span>
        </div>
        <div class="results-bar">
          <div class="results-bar-fill" style="width: ${Math.max(percentage, 0)}%;"></div>
        </div>
      </article>
    `;
  }).join('');

  panoramaEl.style.display = 'block';
}

async function wait(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function loadResultsPanorama() {
  await wait(POST_CONFIRM_DELAY_MS);

  for (let attempt = 0; attempt < RESULTS_RETRY_ATTEMPTS; attempt++) {
    const { ok, data } = await apiFetch(`/votings/${selectedVoting.votingId}/results`);
    if (ok && data) {
      renderResultsPanorama(data);
      return true;
    }

    if (attempt < RESULTS_RETRY_ATTEMPTS - 1) {
      await wait(RESULTS_RETRY_INTERVAL_MS);
    }
  }

  return false;
}

function setVoteInteractionLocked(locked, options = {}) {
  voteFlowLocked = locked;

  const interactionEl = document.getElementById('vote-interaction');
  const formCardEl = document.getElementById('vote-form-card');
  const nextStepEl = document.getElementById('vote-next-step');
  const candidateOptions = document.querySelectorAll('.candidate-option');
  const candidateInputs = document.querySelectorAll('input[name="candidate"]');
  const buttons = document.querySelectorAll('.slide-vote-thumb, #vote-btn');

  interactionEl.classList.toggle('hidden', Boolean(options.hideInteraction));
  formCardEl.classList.toggle('locked', locked);
  nextStepEl.style.display = options.showNextStep ? 'block' : 'none';

  candidateOptions.forEach(opt => opt.classList.toggle('locked', locked));
  candidateInputs.forEach(input => {
    input.disabled = locked;
  });
  buttons.forEach(button => {
    button.disabled = locked;
  });
}

async function submitVote(candidateId = selectedCandidate, interactionSignal, powRetryAttempt = 0) {
  if (!selectedVoting || !candidateId) {
    showError('Please select a candidate');
    return;
  }

  selectedCandidate = candidateId;
  setVoteInteractionLocked(true);

  const statusEl = document.getElementById('vote-status');
  statusEl.className = 'vote-status submitting';
  statusEl.textContent = 'Submitting...';
  hideError();

  const honeypotValue = getHoneypotValue();
  const payload = {
    candidateId,
    honeypot: honeypotValue,
    clientContext: getClientContext(),
  };
  if (interactionSignal && isVotingInteractionTelemetryEnabled()) {
    payload.interactionSignal = interactionSignal;
  }

  let votePath = `/votings/${selectedVoting.votingId}/votes`;
  if (isVotingPowEnabled()) {
    try {
      const pow = await createPowProof(selectedVoting.votingId, statusEl);
      payload.pow = {
        token: pow.challenge.token,
        nonce: pow.nonce,
      };
      payload.powClientMetrics = {
        ...pow.metrics,
        submitStartedAt: new Date().toISOString(),
      };
      votePath = `/votings/${selectedVoting.votingId}/votes/${pow.challenge.challengeId}`;
    } catch (error) {
      statusEl.className = 'vote-status failed';
      statusEl.textContent = error.message || 'Failed to compute anti-bot proof';
      setVoteInteractionLocked(false);
      renderCandidates();
      renderVoteActionSlot();
      return;
    }
  }

  const { ok, data } = await apiFetch(votePath, {
    method: 'POST',
    body: JSON.stringify(payload),
  });

  if (!ok) {
    if (isVotingPowEnabled() && data?.code === 'pow_expired' && powRetryAttempt < 1) {
      statusEl.className = 'vote-status pending';
      statusEl.textContent = 'Retrying expired challenge...';
      await submitVote(candidateId, interactionSignal, powRetryAttempt + 1);
      return;
    }
    statusEl.className = 'vote-status failed';
    statusEl.textContent = data?.message || 'Failed to submit vote';
    setVoteInteractionLocked(false);
    renderCandidates();
    renderVoteActionSlot();
    return;
  }

  const voteId = data.voteId;
  if (!voteId) {
    statusEl.className = 'vote-status failed';
    statusEl.textContent = 'No vote ID returned';
    setVoteInteractionLocked(false);
    renderCandidates();
    renderVoteActionSlot();
    return;
  }

  statusEl.className = 'vote-status pending';
  statusEl.textContent = 'Pending...';

  const confirmed = await pollVoteStatus(voteId);

  if (confirmed) {
    statusEl.className = 'vote-status confirmed';
    statusEl.textContent = 'Vote Confirmed!';
    setVoteInteractionLocked(true, { hideInteraction: true, showNextStep: true });
    await loadResultsPanorama();
  } else {
    statusEl.className = 'vote-status failed';
    statusEl.textContent = 'Vote Failed';
    hideResultsPanorama();
    setVoteInteractionLocked(false);
    renderCandidates();
    renderVoteActionSlot();
  }
}

async function pollVoteStatus(voteId) {
  const maxAttempts = 50;
  const interval = RESULTS_RETRY_INTERVAL_MS;

  for (let attempt = 0; attempt < maxAttempts; attempt++) {
    await new Promise(resolve => setTimeout(resolve, interval));

    const { ok, data } = await apiFetch(`/votes/${voteId}/status`);
    if (!ok) continue;

    if (data.status === 'WRITTEN') return true;
    if (data.status === 'FAILED') return false;
  }

  return false;
}

function backToList() {
  selectedVoting = null;
  resetVoteState();
  document.getElementById('voting-list-section').style.display = 'block';
  document.getElementById('vote-form-section').style.display = 'none';
  document.getElementById('vote-status').textContent = '';
  document.getElementById('vote-next-step').style.display = 'none';
  renderHoneypotField();
  setVoteInteractionLocked(false);
  hideResultsPanorama();
}

function escapeHtml(str) {
  if (!str) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

document.addEventListener('DOMContentLoaded', () => {
  document.getElementById('vote-form-section').style.display = 'none';
  hideResultsPanorama();
  loadVotings();
});
