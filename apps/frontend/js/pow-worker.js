let argon2RuntimePromise = null;

self.onmessage = async event => {
  try {
    const { assetVersion, challengeId, token, algorithm, difficultyBits, params } = event.data || {};
    const payload = decodeTokenPayload(token);
    if (!challengeId || !payload || payload.challengeId !== challengeId) {
      throw new Error('Invalid challenge payload');
    }

    const normalizedAlgorithm = String(algorithm || payload.algorithm || 'sha256').toLowerCase();

    const challengeParams = params || payload.params || {};
    const targetDifficultyBits = Number(challengeParams.difficultyBits || difficultyBits || payload.difficultyBits || 0);

    if (normalizedAlgorithm === 'sha256') {
      const nonce = await solveSha256Challenge(payload, targetDifficultyBits);
      self.postMessage({ success: true, nonce });
      return;
    }

    if (normalizedAlgorithm === 'argon2id') {
      await ensureArgon2Runtime(assetVersion);
      const nonce = await solveArgon2idChallenge(payload, challengeParams, targetDifficultyBits);
      self.postMessage({ success: true, nonce });
      return;
    }

    throw new Error(`Unsupported proof-of-work algorithm: ${normalizedAlgorithm}`);
  } catch (error) {
    self.postMessage({ success: false, error: error?.message || 'Worker error' });
  }
};

async function ensureArgon2Runtime(assetVersion) {
  if (!argon2RuntimePromise) {
    const versionedWasmPath = withAssetVersion('/js/vendor/argon2.wasm', assetVersion);
    self.argon2WasmPath = versionedWasmPath;
    argon2RuntimePromise = Promise.resolve().then(() => {
      importScripts(withAssetVersion('/js/vendor/argon2-bundled.min.js', assetVersion));
    });
  }
  return argon2RuntimePromise;
}

async function solveSha256Challenge(payload, difficultyBits) {
  let nonce = 0n;
  while (true) {
    const nonceString = nonce.toString(10);
    const material = `${payload.challengeId}:${payload.votingId}:${payload.salt}:${nonceString}`;
    const hashBuffer = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(material));
    const hash = new Uint8Array(hashBuffer);
    if (hasLeadingZeroBits(hash, difficultyBits)) {
      return nonceString;
    }
    nonce += 1n;
  }
}

async function solveArgon2idChallenge(payload, params, difficultyBits) {
  if (!self.argon2 || typeof self.argon2.hash !== 'function') {
    throw new Error('Argon2 runtime is unavailable');
  }

  const memoryKiB = Number(params?.memoryKiB || 8192);
  const timeCost = Number(params?.timeCost || 1);
  const parallelism = Number(params?.parallelism || 1);
  const hashLength = Number(params?.hashLength || 32);
  let nonce = 0n;

  while (true) {
    const nonceString = nonce.toString(10);
    const material = `${payload.challengeId}:${payload.votingId}:${payload.salt}:${nonceString}`;
    const result = await self.argon2.hash({
      pass: material,
      salt: payload.salt,
      time: timeCost,
      mem: memoryKiB,
      hashLen: hashLength,
      parallelism,
      type: self.argon2.ArgonType.Argon2id,
    });
    if (hasLeadingZeroBits(result.hash, difficultyBits)) {
      return nonceString;
    }
    nonce += 1n;
  }
}

function decodeTokenPayload(token) {
  const [rawPayload] = String(token || '').split('.');
  if (!rawPayload) return null;
  const decoded = atob(base64UrlToBase64(rawPayload));
  return JSON.parse(decoded);
}

function base64UrlToBase64(value) {
  const padded = value.replace(/-/g, '+').replace(/_/g, '/');
  const remainder = padded.length % 4;
  if (remainder === 0) return padded;
  return padded + '='.repeat(4 - remainder);
}

function withAssetVersion(path, assetVersion) {
  const version = String(assetVersion || '').trim();
  if (!version) {
    return path;
  }
  const separator = path.includes('?') ? '&' : '?';
  return `${path}${separator}v=${encodeURIComponent(version)}`;
}

function hasLeadingZeroBits(hash, bits) {
  if (!bits || bits <= 0) return true;
  const fullBytes = Math.floor(bits / 8);
  const remainingBits = bits % 8;
  for (let i = 0; i < fullBytes; i += 1) {
    if (hash[i] !== 0) {
      return false;
    }
  }
  if (remainingBits === 0) {
    return true;
  }
  const mask = 0xff << (8 - remainingBits);
  return (hash[fullBytes] & mask) === 0;
}
