import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Counter, Rate } from 'k6/metrics';

const BASE_URL = __ENV.API_URL || 'http://localhost:8080';
const VOTING_ID = __ENV.VOTING_ID || null;
const SELECTED_SCENARIO = __ENV.SCENARIO || 'all';
const MODE = __ENV.MODE || 'perf';
const THINK_TIME = Number(__ENV.THINK_TIME || '0.02');
const CONSISTENCY_ITERATIONS = Number(__ENV.CONSISTENCY_ITERATIONS || '200');
const CONSISTENCY_TIMEOUT_MS = Number(__ENV.CONSISTENCY_TIMEOUT_MS || '20000');
const CONSISTENCY_POLL_INTERVAL_MS = Number(__ENV.CONSISTENCY_POLL_INTERVAL_MS || '250');
function sustainableScenario(name, target, plateauDuration) {
  return {
    executor: 'ramping-vus',
    startVUs: 0,
    stages: [
      { duration: '45s', target: Math.max(1, Math.floor(target / 2)) },
      { duration: '45s', target },
      { duration: plateauDuration, target },
      { duration: '45s', target: 0 },
    ],
    tags: { test_type: name },
  };
}

const consistencyVotesSent = new Counter('consistency_votes_sent');
const consistencyVotesAccepted = new Counter('consistency_votes_202');
const consistencyVotesNon202 = new Counter('consistency_votes_non_202');
const voteStatus202 = new Rate('vote_status_202');

const ALL_SCENARIOS = {
  smoke: {
    executor: 'constant-vus',
    vus: 1,
    duration: '10s',
    tags: { test_type: 'smoke' },
  },
  sustained: {
    executor: 'ramping-vus',
    startVUs: 0,
    stages: [
      { duration: '30s', target: 50 },
      { duration: '1m', target: 50 },
      { duration: '30s', target: 0 },
    ],
    tags: { test_type: 'sustained' },
  },
  spike: {
    executor: 'ramping-vus',
    startVUs: 5,
    stages: [
      { duration: '10s', target: 5 },
      { duration: '20s', target: 200 },
      { duration: '10s', target: 5 },
      { duration: '30s', target: 5 },
    ],
    tags: { test_type: 'spike' },
  },
  stress: sustainableScenario('stress', 900, '3m'),
  consistency: {
    executor: 'shared-iterations',
    vus: 1,
    iterations: CONSISTENCY_ITERATIONS,
    maxDuration: '2m',
    tags: { test_type: 'consistency' },
  },
};

const scenarios = MODE === 'consistency'
  ? { consistency: ALL_SCENARIOS.consistency }
  : (SELECTED_SCENARIO === 'all'
    ? {
      smoke: ALL_SCENARIOS.smoke,
      sustained: ALL_SCENARIOS.sustained,
      spike: ALL_SCENARIOS.spike,
      stress: ALL_SCENARIOS.stress,
    }
    : (ALL_SCENARIOS[SELECTED_SCENARIO]
      ? { [SELECTED_SCENARIO]: ALL_SCENARIOS[SELECTED_SCENARIO] }
      : {
        smoke: ALL_SCENARIOS.smoke,
        sustained: ALL_SCENARIOS.sustained,
        spike: ALL_SCENARIOS.spike,
        stress: ALL_SCENARIOS.stress,
      }));

const thresholds = MODE === 'consistency'
  ? {
    http_req_duration: ['p(95)<1000'],
    http_req_failed: ['rate<0.05'],
    vote_status_202: ['rate==1'],
    consistency_votes_non_202: ['count==0'],
  }
  : {
    http_req_duration: ['p(95)<1000'],
    http_req_failed: ['rate<0.2'],
  };

export const options = {
  scenarios,
  thresholds,
};

function getVotingId() {
  if (VOTING_ID) {
    return VOTING_ID;
  }

  const createRes = http.post(
    `${BASE_URL}/votings`,
    JSON.stringify({
      name: `Load Test ${Date.now()}`,
      candidates: [
        { candidateId: 'c1', name: 'Candidate 1' },
        { candidateId: 'c2', name: 'Candidate 2' },
      ],
    }),
    { headers: { 'Content-Type': 'application/json' } }
  );

  check(createRes, {
    'create voting status 201': (r) => r.status === 201,
  });

  const voting = JSON.parse(createRes.body);
  const votingId = voting.votingId;

  const openRes = http.patch(
    `${BASE_URL}/votings/${votingId}`,
    JSON.stringify({ status: 'OPEN' }),
    { headers: { 'Content-Type': 'application/json' } }
  );

  check(openRes, {
    'open voting status 200': (r) => r.status === 200,
  });

  sleep(1);

  return votingId;
}

export function setup() {
  const votingId = getVotingId();
  console.log(`Using voting ID: ${votingId}`);
  const expectedTotalVotes = MODE === 'consistency' ? CONSISTENCY_ITERATIONS : 0;
  const expectedByCandidate = {
    c1: MODE === 'consistency' ? Math.ceil(CONSISTENCY_ITERATIONS / 2) : 0,
    c2: MODE === 'consistency' ? Math.floor(CONSISTENCY_ITERATIONS / 2) : 0,
  };
  return { votingId, expectedTotalVotes, expectedByCandidate };
}

export default function(data) {
  const votingId = data.votingId;
  const ip = `203.0.113.${Math.floor(Math.random() * 256)}.${Math.floor(Math.random() * 256)}`;

  if (MODE === 'consistency') {
    const candidateId = __ITER % 2 === 0 ? 'c1' : 'c2';
    const res = http.post(
      `${BASE_URL}/votings/${votingId}/votes`,
      JSON.stringify({
        candidateId,
        ip,
      }),
      { headers: { 'Content-Type': 'application/json' } }
    );

    consistencyVotesSent.add(1);
    const accepted = res.status === 202;
    voteStatus202.add(accepted);
    if (accepted) {
      consistencyVotesAccepted.add(1);
    } else {
      consistencyVotesNon202.add(1);
    }

    check(res, {
      'vote status 202': (r) => r.status === 202,
    });
    return;
  }

  const candidateId = `c${Math.floor(Math.random() * 2) + 1}`;

  group('vote', () => {
    const res = http.post(
      `${BASE_URL}/votings/${votingId}/votes`,
      JSON.stringify({
        candidateId: candidateId,
        ip: ip,
      }),
      { headers: { 'Content-Type': 'application/json' } }
    );

    check(res, {
      'vote accepted or blocked': (r) => [202, 403, 409].includes(r.status),
    });
  });

  group('results', () => {
    const res = http.get(`${BASE_URL}/votings/${votingId}/results`);

    check(res, {
      'results status 200': (r) => r.status === 200,
    });
  });

  sleep(THINK_TIME);
}

export function teardown(data) {
  if (MODE !== 'consistency') {
    return;
  }

  const votingId = data.votingId;
  const expectedTotalVotes = data.expectedTotalVotes;
  const expectedByCandidate = data.expectedByCandidate;
  const deadline = Date.now() + CONSISTENCY_TIMEOUT_MS;

  while (Date.now() < deadline) {
    const res = http.get(`${BASE_URL}/votings/${votingId}/results`);
    if (res.status === 200) {
      const body = JSON.parse(res.body);
      const totalVotes = Number(body.totalVotes || 0);
      const byCandidate = body.byCandidate || {};
      const c1 = Number(byCandidate.c1 || 0);
      const c2 = Number(byCandidate.c2 || 0);

      if (
        totalVotes === expectedTotalVotes &&
        c1 === expectedByCandidate.c1 &&
        c2 === expectedByCandidate.c2
      ) {
        return;
      }
    }
    sleep(CONSISTENCY_POLL_INTERVAL_MS / 1000);
  }

  throw new Error('consistency verification failed before timeout');
}

export function handleSummary(data) {
  return {
    'stdout': textSummary(data, { indent: ' ', enableColors: true }),
  };
}

function textSummary(data, opts) {
  const indent = opts.indent || '';
  const totalRequests = data.metrics.http_reqs?.values?.count ?? 0;
  const failedRate = data.metrics.http_req_failed?.values?.rate ?? 0;
  const failedRequests = Math.round(totalRequests * failedRate);
  const p95 = data.metrics.http_req_duration?.values?.['p(95)'] ?? 0;
  const p99 = data.metrics.http_req_duration?.values?.['p(99)'] ?? 0;
  const errorRate = data.metrics.errors?.values?.rate ?? failedRate;
  let output = `\n${indent}=== Load Test Results ===\n\n`;

  output += `${indent}Total Requests: ${totalRequests}\n`;
  output += `${indent}Failed Requests: ${failedRequests}\n`;
  output += `${indent}Request Duration P95: ${p95.toFixed(2)}ms\n`;
  output += `${indent}Request Duration P99: ${p99.toFixed(2)}ms\n`;
  output += `${indent}Error Rate: ${(errorRate * 100).toFixed(2)}%\n`;

  if (MODE === 'consistency') {
    const sent = data.metrics.consistency_votes_sent?.values?.count ?? 0;
    const accepted = data.metrics.consistency_votes_202?.values?.count ?? 0;
    const non202 = data.metrics.consistency_votes_non_202?.values?.count ?? 0;
    output += `${indent}Consistency Votes Sent: ${sent}\n`;
    output += `${indent}Consistency Votes Accepted (202): ${accepted}\n`;
    output += `${indent}Consistency Votes Non-202: ${non202}\n`;
  }

  return output;
}
