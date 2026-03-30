import "@typespec/http";
import "@typespec/rest";
import "@typespec/openapi3";

using TypeSpec.Http;
using TypeSpec.Rest;
using TypeSpec.OpenAPI;

@service({ title: "Voting Platform API" })
@server("http://localhost:3000/api", "Frontend edge (recommended)")
@server("http://localhost:8080", "Direct API (local/internal)")
namespace VotingPlatform.Api;

scalar ULID extends string;

alias VotingStatus = "CREATED" | "OPEN" | "CLOSED" | "CANCELLED";
alias PolicyTargetType = "IP";
alias PolicyAction = "ACTIVATE" | "DEACTIVATE";
alias PolicyEffectiveMode = "FORWARD_ONLY" | "RETROACTIVE";
alias SlideVoteMode = "off" | "button" | "full";
alias PowAlgorithm = "sha256" | "argon2id";
alias VoteDeliveryStatus = "PENDING" | "WRITTEN" | "FAILED";

model ErrorResponse {
  code: string;
  message: string;
}

model Candidate {
  candidateId: string;
  name: string;
}

model PowConfig {
  enabled: boolean;
  algorithm?: PowAlgorithm;
  ttlSeconds: int32;
  baseDifficultyBits: int32;
  maxDifficultyBits: int32;
  adaptiveWindowSeconds: int32;
  baseMemoryKiB?: int32;
  memoryGrowthFactor?: float64;
  difficultyStepEvery?: int32;
  memoryKiB?: int32;
  timeCost?: int32;
  parallelism?: int32;
  hashLength?: int32;
}

model PowConfigPatch {
  enabled?: boolean;
  algorithm?: PowAlgorithm;
  ttlSeconds?: int32;
  baseDifficultyBits?: int32;
  maxDifficultyBits?: int32;
  adaptiveWindowSeconds?: int32;
  baseMemoryKiB?: int32;
  memoryGrowthFactor?: float64;
  difficultyStepEvery?: int32;
  memoryKiB?: int32;
  timeCost?: int32;
  parallelism?: int32;
  hashLength?: int32;
}

model AntiAbuseConfig {
  honeypotEnabled: boolean;
  slideVoteMode: SlideVoteMode;
  interactionTelemetryEnabled: boolean;
  pow: PowConfig;
}

model AntiAbuseConfigPatch {
  honeypotEnabled?: boolean;
  slideVoteMode?: SlideVoteMode;
  interactionTelemetryEnabled?: boolean;
  pow?: PowConfigPatch;
}

model Voting {
  votingId: ULID;
  name: string;
  status: VotingStatus;
  candidates: Candidate[];
  startsAt?: utcDateTime;
  endsAt?: utcDateTime;
  antiAbuse: AntiAbuseConfig;
  createdAt: utcDateTime;
  updatedAt?: utcDateTime;
}

model CreateVotingRequest {
  name: string;
  status?: VotingStatus;
  candidates: Candidate[];
  startsAt?: utcDateTime;
  endsAt?: utcDateTime;
  antiAbuse?: AntiAbuseConfig;
}

model UpdateVotingRequest {
  name?: string;
  status?: VotingStatus;
  startsAt?: utcDateTime;
  endsAt?: utcDateTime;
  candidates?: Candidate[];
  antiAbuse?: AntiAbuseConfigPatch;
}

model VoteProof {
  token: string;
  nonce: string;
}

model VoteInteractionSignal {
  openedAt?: utcDateTime;
  startedAt?: utcDateTime;
  completedAt?: utcDateTime;
  openToStartMs?: int32;
  gestureDurationMs?: int32;
  moveEvents?: int32;
  maxProgress?: float64;
  completed?: boolean;
  cancelled?: boolean;
  mode?: SlideVoteMode;
}

model VotePowClientMetrics {
  challengeId?: string;
  algorithm?: PowAlgorithm;
  challengeReceivedAt?: utcDateTime;
  solveStartedAt?: utcDateTime;
  solveCompletedAt?: utcDateTime;
  submitStartedAt?: utcDateTime;
  solveDurationMs?: int32;
  retryAttempt?: int32;
}

model VoteClientContext {
  userAgent?: string;
  platform?: string;
  language?: string;
  languages?: string[];
  screenWidth?: int32;
  screenHeight?: int32;
  viewportWidth?: int32;
  viewportHeight?: int32;
  devicePixelRatio?: float64;
  maxTouchPoints?: int32;
  timezone?: string;
  mobile?: boolean;
}

model VoteRequest {
  candidateId: string;
  ip?: string;
  honeypot?: string;
  pow?: VoteProof;
  interactionSignal?: VoteInteractionSignal;
  powClientMetrics?: VotePowClientMetrics;
  clientContext?: VoteClientContext;
}

model VoteAcceptedResponse {
  status: VoteDeliveryStatus;
  voteId: ULID;
  votingId: ULID;
  message: string;
}

model PowChallengeParams {
  difficultyBits?: int32;
  memoryKiB?: int32;
  timeCost?: int32;
  parallelism?: int32;
  hashLength?: int32;
}

model VoteChallengeResponse {
  challengeId: string;
  token: string;
  algorithm: PowAlgorithm;
  difficultyBits?: int32;
  params?: PowChallengeParams;
  expiresAt: utcDateTime;
}

model VoteStatusResponse {
  voteId: ULID;
  votingId: ULID;
  status: VoteDeliveryStatus;
  updatedAt: utcDateTime;
}

model ResultsResponse {
  votingId: ULID;
  totalVotes: int64;
  byCandidate: Record<int64>;
  percentageByCandidate: Record<float64>;
  byHour: Record<int64>;
  checkpoint?: string;
  updatedAt: utcDateTime;
}

model PolicyRequest {
  targetType: PolicyTargetType;
  targetValue: string;
  action: PolicyAction;
  effectiveMode: PolicyEffectiveMode;
  reason?: string;
}

model PolicyResponse {
  policyEventId: ULID;
  votingId: ULID;
  targetType: PolicyTargetType;
  targetValue: string;
  action: PolicyAction;
  effectiveMode: PolicyEffectiveMode;
  createdAt: utcDateTime;
}

@route("/healthz")
@tag("Operational")
op healthz(): { status: string };

@route("/metrics")
@tag("Operational")
op metrics(): string;

@route("/votings")
@tag("Votings")
interface VotingsApi {
  @post createVoting(@body body: CreateVotingRequest): {
    @statusCode statusCode: 201;
    @body body: Voting;
  } | {
    @statusCode statusCode: 400;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 500;
    @body body: ErrorResponse;
  };

  @get listVotings(@query status?: VotingStatus): Voting[];
}

@route("/votings/{votingId}")
@tag("Votings")
interface VotingApi {
  @get getVoting(@path votingId: ULID): Voting | {
    @statusCode statusCode: 404;
    @body body: ErrorResponse;
  };

  @patch updateVoting(@path votingId: ULID, @body body: UpdateVotingRequest): Voting | {
    @statusCode statusCode: 400;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 404;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 500;
    @body body: ErrorResponse;
  };
}

@route("/votings/{votingId}/vote-challenges")
@tag("Votes")
interface VoteChallengeApi {
  @doc("Issues a proof-of-work challenge for an OPEN voting when PoW is enabled. Returns 404 when the voting does not exist or PoW is disabled, 409 when the voting is not OPEN, and 400 when the voting window has not started or has already ended.")
  @post createVoteChallenge(@path votingId: ULID): {
    @statusCode statusCode: 201;
    @body body: VoteChallengeResponse;
  } | {
    @statusCode statusCode: 400;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 404;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 409;
    @body body: ErrorResponse;
  };
}

@route("/votings/{votingId}/votes")
@tag("Votes")
interface VotesApi {
  @doc("Registers a vote. When proof-of-work is enabled for the voting, requests without a valid challenge submission may fail with 400 or 403. When the recommended edge path is used, the frontend proxy may also reject requests with 429 before they reach the API.")
  @post registerVote(@path votingId: ULID, @body body: VoteRequest, @query confirm?: boolean, @query timeoutMs?: int32): {
    @statusCode statusCode: 202;
    @body body: VoteAcceptedResponse;
  } | {
    @statusCode statusCode: 400;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 403;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 404;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 409;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 429;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 500;
    @body body: ErrorResponse;
  };
}

@route("/votings/{votingId}/votes/{challengeId}")
@tag("Votes")
interface VotesWithChallengeApi {
  @doc("Registers a vote tied to a previously issued proof-of-work challenge. This is the required route when PoW is enabled for the target voting.")
  @post registerVoteWithChallenge(@path votingId: ULID, @path challengeId: string, @body body: VoteRequest, @query confirm?: boolean, @query timeoutMs?: int32): {
    @statusCode statusCode: 202;
    @body body: VoteAcceptedResponse;
  } | {
    @statusCode statusCode: 400;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 403;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 404;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 409;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 429;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 500;
    @body body: ErrorResponse;
  };
}

@route("/votes/{voteId}/status")
@tag("Votes")
interface VoteStatusApi {
  @get getVoteStatus(@path voteId: ULID): VoteStatusResponse | {
    @statusCode statusCode: 404;
    @body body: ErrorResponse;
  };
}

@route("/votings/{votingId}/results")
@tag("Results")
interface ResultsApi {
  @doc("Returns the latest projected snapshot for the voting. Until a snapshot is consumed, the API returns a zero-state result with the correct candidate keys and empty hourly buckets.")
  @get getResults(@path votingId: ULID): ResultsResponse | {
    @statusCode statusCode: 404;
    @body body: ErrorResponse;
  };
}

@route("/votings/{votingId}/policies")
@tag("Policies")
interface PolicyApi {
  @post createPolicy(@path votingId: ULID, @body body: PolicyRequest): {
    @statusCode statusCode: 201;
    @body body: PolicyResponse;
  } | {
    @statusCode statusCode: 400;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 404;
    @body body: ErrorResponse;
  } | {
    @statusCode statusCode: 500;
    @body body: ErrorResponse;
  };
}
