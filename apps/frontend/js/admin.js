let votings = [];
let editingVoting = null;
let showingResults = null;

function getDefaultAntiAbuse() {
  return {
    honeypotEnabled: true,
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

function normalizeAntiAbuse(antiAbuse) {
  const defaults = getDefaultAntiAbuse();
  return {
    honeypotEnabled: antiAbuse?.honeypotEnabled ?? defaults.honeypotEnabled,
    slideVoteMode: antiAbuse?.slideVoteMode || defaults.slideVoteMode,
    interactionTelemetryEnabled: antiAbuse?.interactionTelemetryEnabled ?? defaults.interactionTelemetryEnabled,
    pow: {
      enabled: antiAbuse?.pow?.enabled ?? defaults.pow.enabled,
      algorithm: antiAbuse?.pow?.algorithm ?? defaults.pow.algorithm,
      ttlSeconds: antiAbuse?.pow?.ttlSeconds ?? defaults.pow.ttlSeconds,
      baseDifficultyBits: antiAbuse?.pow?.baseDifficultyBits ?? defaults.pow.baseDifficultyBits,
      maxDifficultyBits: antiAbuse?.pow?.maxDifficultyBits ?? defaults.pow.maxDifficultyBits,
      adaptiveWindowSeconds: antiAbuse?.pow?.adaptiveWindowSeconds ?? defaults.pow.adaptiveWindowSeconds,
      memoryKiB: antiAbuse?.pow?.memoryKiB ?? defaults.pow.memoryKiB,
      timeCost: antiAbuse?.pow?.timeCost ?? defaults.pow.timeCost,
      parallelism: antiAbuse?.pow?.parallelism ?? defaults.pow.parallelism,
      hashLength: antiAbuse?.pow?.hashLength ?? defaults.pow.hashLength,
    },
  };
}

function populateAntiAbuseForm(antiAbuse) {
  const normalized = normalizeAntiAbuse(antiAbuse);
  document.getElementById('antiabuse-honeypot-enabled').checked = normalized.honeypotEnabled;
  document.getElementById('antiabuse-slide-vote-mode').value = normalized.slideVoteMode;
  document.getElementById('antiabuse-interaction-telemetry-enabled').checked = normalized.interactionTelemetryEnabled;
  document.getElementById('antiabuse-pow-enabled').checked = normalized.pow.enabled;
  document.getElementById('antiabuse-pow-algorithm').value = normalized.pow.algorithm;
  document.getElementById('antiabuse-pow-ttl-seconds').value = normalized.pow.ttlSeconds;
  document.getElementById('antiabuse-pow-base-difficulty-bits').value = normalized.pow.baseDifficultyBits;
  document.getElementById('antiabuse-pow-max-difficulty-bits').value = normalized.pow.maxDifficultyBits;
  document.getElementById('antiabuse-pow-adaptive-window-seconds').value = normalized.pow.adaptiveWindowSeconds;
  document.getElementById('antiabuse-pow-memory-kib').value = normalized.pow.memoryKiB;
  document.getElementById('antiabuse-pow-time-cost').value = normalized.pow.timeCost;
  document.getElementById('antiabuse-pow-parallelism').value = normalized.pow.parallelism;
  document.getElementById('antiabuse-pow-hash-length').value = normalized.pow.hashLength;
  syncPowFieldsState();
}

function readAntiAbuseForm() {
  return {
    honeypotEnabled: document.getElementById('antiabuse-honeypot-enabled').checked,
    slideVoteMode: document.getElementById('antiabuse-slide-vote-mode').value,
    interactionTelemetryEnabled: document.getElementById('antiabuse-interaction-telemetry-enabled').checked,
    pow: {
      enabled: document.getElementById('antiabuse-pow-enabled').checked,
      algorithm: document.getElementById('antiabuse-pow-algorithm').value,
      ttlSeconds: Number(document.getElementById('antiabuse-pow-ttl-seconds').value),
      baseDifficultyBits: Number(document.getElementById('antiabuse-pow-base-difficulty-bits').value),
      maxDifficultyBits: Number(document.getElementById('antiabuse-pow-max-difficulty-bits').value),
      adaptiveWindowSeconds: Number(document.getElementById('antiabuse-pow-adaptive-window-seconds').value),
      memoryKiB: Number(document.getElementById('antiabuse-pow-memory-kib').value),
      timeCost: Number(document.getElementById('antiabuse-pow-time-cost').value),
      parallelism: Number(document.getElementById('antiabuse-pow-parallelism').value),
      hashLength: Number(document.getElementById('antiabuse-pow-hash-length').value),
    },
  };
}

function syncPowFieldsState() {
  const powEnabled = document.getElementById('antiabuse-pow-enabled').checked;
  const powAlgorithm = document.getElementById('antiabuse-pow-algorithm').value;
  const card = document.getElementById('pow-config-card');
  card.classList.toggle('inactive', !powEnabled);

  [
    'antiabuse-pow-algorithm',
    'antiabuse-pow-ttl-seconds',
    'antiabuse-pow-base-difficulty-bits',
    'antiabuse-pow-max-difficulty-bits',
    'antiabuse-pow-adaptive-window-seconds',
  ].forEach(id => {
    document.getElementById(id).disabled = !powEnabled;
  });

  const argon2Ids = [
    'antiabuse-pow-memory-kib',
    'antiabuse-pow-time-cost',
    'antiabuse-pow-parallelism',
    'antiabuse-pow-hash-length',
  ];
  const argon2Enabled = powEnabled && powAlgorithm === 'argon2id';
  argon2Ids.forEach(id => {
    const input = document.getElementById(id);
    input.disabled = !argon2Enabled;
    input.closest('.form-group')?.classList.toggle('pow-optional-disabled', !argon2Enabled);
  });
}

async function loadVotings() {
  const listEl = document.getElementById('voting-list');
  listEl.innerHTML = '<tr><td colspan="5">Loading...</td></tr>';

  const { ok, data } = await apiFetch('/votings');
  if (!ok) {
    listEl.innerHTML = '<tr><td colspan="5" class="error">Failed to load votings</td></tr>';
    return;
  }

  votings = Array.isArray(data) ? data : [];
  renderVotingList();
}

function renderVotingList() {
  const listEl = document.getElementById('voting-list');

  if (votings.length === 0) {
    listEl.innerHTML = '<tr><td colspan="5">No votings found. Create one to get started.</td></tr>';
    return;
  }

  listEl.innerHTML = votings.map(v => `
    <tr>
      <td>${escapeHtml(v.votingId)}</td>
      <td>${escapeHtml(v.name)}</td>
      <td><span class="status-badge status-${v.status}">${v.status}</span></td>
      <td>${v.candidates ? v.candidates.length : 0}</td>
      <td class="actions">
        <button class="btn-small btn-view" onclick="viewResults('${v.votingId}')">Results</button>
        <button class="btn-small btn-edit" onclick="editVoting('${v.votingId}')">Edit</button>
      </td>
    </tr>
  `).join('');
}

function showCreateForm() {
  editingVoting = null;
  document.getElementById('form-title').textContent = 'Create New Voting';
  document.getElementById('voting-id-field').style.display = 'none';
  document.getElementById('form-section').style.display = 'block';
  document.getElementById('results-section').style.display = 'none';
  clearForm();
}

function editVoting(votingId) {
  const voting = votings.find(v => v.votingId === votingId);
  if (!voting) return;

  editingVoting = voting;
  document.getElementById('form-title').textContent = 'Edit Voting';
  document.getElementById('voting-id-field').style.display = 'block';
  document.getElementById('voting-id-display').textContent = voting.votingId;
  document.getElementById('form-section').style.display = 'block';
  document.getElementById('results-section').style.display = 'none';

  document.getElementById('voting-name').value = voting.name || '';
  document.getElementById('voting-status').value = voting.status || 'CREATED';

  if (voting.startsAt) {
    document.getElementById('voting-starts-at').value = voting.startsAt.slice(0, 16);
  } else {
    document.getElementById('voting-starts-at').value = '';
  }

  if (voting.endsAt) {
    document.getElementById('voting-ends-at').value = voting.endsAt.slice(0, 16);
  } else {
    document.getElementById('voting-ends-at').value = '';
  }

  renderCandidateInputs(voting.candidates || []);
  populateAntiAbuseForm(voting.antiAbuse);
}

function renderCandidateInputs(candidates) {
  const container = document.getElementById('candidates-container');
  container.innerHTML = candidates.map((c, i) => `
    <div class="candidate-row">
      <input type="text" placeholder="Candidate ID" class="candidate-id" value="${escapeHtml(c.candidateId || '')}">
      <input type="text" placeholder="Name" class="candidate-name" value="${escapeHtml(c.name || '')}">
      <button type="button" class="btn-remove" onclick="removeCandidateRow(this)">×</button>
    </div>
  `).join('');
}

function addCandidateRow() {
  const container = document.getElementById('candidates-container');
  const div = document.createElement('div');
  div.className = 'candidate-row';
  div.innerHTML = `
    <input type="text" placeholder="Candidate ID" class="candidate-id">
    <input type="text" placeholder="Name" class="candidate-name">
    <button type="button" class="btn-remove" onclick="removeCandidateRow(this)">×</button>
  `;
  container.appendChild(div);
}

function removeCandidateRow(btn) {
  btn.parentElement.remove();
}

function clearForm() {
  document.getElementById('voting-name').value = '';
  document.getElementById('voting-status').value = 'CREATED';
  document.getElementById('voting-starts-at').value = '';
  document.getElementById('voting-ends-at').value = '';
  renderCandidateInputs([{ candidateId: '', name: '' }]);
  populateAntiAbuseForm(getDefaultAntiAbuse());
}

function cancelForm() {
  document.getElementById('form-section').style.display = 'none';
  editingVoting = null;
}

async function saveVoting() {
  const name = document.getElementById('voting-name').value.trim();
  if (!name) {
    showError('Name is required');
    return;
  }

  const candidateRows = document.querySelectorAll('.candidate-row');
  const candidates = [];
  candidateRows.forEach(row => {
    const id = row.querySelector('.candidate-id').value.trim();
    const cname = row.querySelector('.candidate-name').value.trim();
    if (id && cname) {
      candidates.push({ candidateId: id, name: cname });
    }
  });

  if (candidates.length === 0) {
    showError('At least one candidate is required');
    return;
  }

  const payload = {
    name,
    candidates,
    antiAbuse: readAntiAbuseForm(),
  };

  if (payload.antiAbuse.pow.baseDifficultyBits > payload.antiAbuse.pow.maxDifficultyBits) {
    showError('PoW base difficulty must be less than or equal to max difficulty');
    return;
  }
  if (!['sha256', 'argon2id'].includes(payload.antiAbuse.pow.algorithm)) {
    showError('PoW algorithm must be sha256 or argon2id');
    return;
  }

  const status = document.getElementById('voting-status').value;
  if (status) {
    payload.status = status;
  }

  const startsAt = document.getElementById('voting-starts-at').value;
  if (startsAt) payload.startsAt = new Date(startsAt).toISOString();

  const endsAt = document.getElementById('voting-ends-at').value;
  if (endsAt) payload.endsAt = new Date(endsAt).toISOString();

  let url = '/votings';
  let method = 'POST';

  if (editingVoting) {
    url = `/votings/${editingVoting.votingId}`;
    method = 'PATCH';
  }

  const { ok } = await apiFetch(url, {
    method,
    body: JSON.stringify(payload),
  });

  if (!ok) {
    showError('Failed to save voting');
    return;
  }

  document.getElementById('form-section').style.display = 'none';
  loadVotings();
}

async function viewResults(votingId) {
  const voting = votings.find(v => v.votingId === votingId);
  if (!voting) return;

  showingResults = voting;
  document.getElementById('form-section').style.display = 'none';
  document.getElementById('results-voting-name').textContent = voting.name;
  document.getElementById('results-section').style.display = 'block';

  const { ok, data } = await apiFetch(`/votings/${votingId}/results`);

  const resultsEl = document.getElementById('results-body');
  if (!ok || !data.byCandidate) {
    resultsEl.innerHTML = '<tr><td colspan="3">No results available</td></tr>';
    return;
  }

  const byCandidate = data.byCandidate || {};
  const totalVotes = data.totalVotes || 0;
  const candidates = voting.candidates || [];

  const results = candidates.map(c => ({
    candidateId: c.candidateId,
    name: c.name,
    count: byCandidate[c.candidateId] || 0
  }));

  resultsEl.innerHTML = results.map(r => `
    <tr>
      <td>${escapeHtml(r.candidateId)}</td>
      <td>${escapeHtml(r.name)}</td>
      <td>${r.count}</td>
    </tr>
  `).join('') + `
    <tr class="total-row">
      <td colspan="2">Total</td>
      <td>${totalVotes}</td>
    </tr>
  `;
}

function closeResults() {
  document.getElementById('results-section').style.display = 'none';
  showingResults = null;
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
  document.getElementById('form-section').style.display = 'none';
  document.getElementById('results-section').style.display = 'none';
  document.getElementById('antiabuse-pow-enabled').addEventListener('change', syncPowFieldsState);
  document.getElementById('antiabuse-pow-algorithm').addEventListener('change', syncPowFieldsState);
  loadVotings();
});
