package support

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

type PoWToken struct {
	ChallengeID    string `json:"challengeId"`
	VotingID       string `json:"votingId"`
	Salt           string `json:"salt"`
	DifficultyBits int    `json:"difficultyBits"`
	Params         *struct {
		DifficultyBits int `json:"difficultyBits"`
	} `json:"params"`
}

func ParsePoWToken(token string) (*PoWToken, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid token format")
	}

	payloadRaw := parts[0]
	padded := payloadRaw
	switch len(payloadRaw) % 4 {
	case 2:
		padded += "=="
	case 3:
		padded += "="
	}
	padded = strings.ReplaceAll(strings.ReplaceAll(padded, "-", "+"), "_", "/")
	payloadDecoded, err := base64.StdEncoding.DecodeString(padded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode token: %w", err)
	}

	var powToken PoWToken
	if err := json.Unmarshal(payloadDecoded, &powToken); err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	return &powToken, nil
}

func valid(hashBytes []byte, bits int) bool {
	full := bits / 8
	rem := bits % 8

	for i := 0; i < full; i++ {
		if hashBytes[i] != 0 {
			return false
		}
	}
	if rem == 0 {
		return true
	}
	mask := byte((0xFF << (8 - rem)) & 0xFF)
	return (hashBytes[full] & mask) == 0
}

func SolvePoW(token string) (string, error) {
	powToken, err := ParsePoWToken(token)
	if err != nil {
		return "", err
	}

	bits := powToken.DifficultyBits
	if powToken.Params != nil && powToken.Params.DifficultyBits > 0 {
		bits = powToken.Params.DifficultyBits
	}

	prefix := fmt.Sprintf("%s:%s:%s:", powToken.ChallengeID, powToken.VotingID, powToken.Salt)

	for nonce := 0; nonce < 20_000_000; nonce++ {
		nonceStr := fmt.Sprintf("%d", nonce)
		hash := sha256.Sum256([]byte(prefix + nonceStr))
		if valid(hash[:], bits) {
			return nonceStr, nil
		}
	}

	return "", fmt.Errorf("nonce not found within search limit")
}

func SolvePoWDistributed(token string) (string, error) {
	powToken, err := ParsePoWToken(token)
	if err != nil {
		return "", err
	}

	bits := powToken.DifficultyBits
	if powToken.Params != nil && powToken.Params.DifficultyBits > 0 {
		bits = powToken.Params.DifficultyBits
	}

	prefix := fmt.Sprintf("%s:%s:%s:", powToken.ChallengeID, powToken.VotingID, powToken.Salt)

	for nonce := 0; nonce < 5_000_000; nonce++ {
		nonceStr := fmt.Sprintf("%d", nonce)
		hash := sha256.Sum256([]byte(prefix + nonceStr))
		if valid(hash[:], bits) {
			return nonceStr, nil
		}
	}

	return "", fmt.Errorf("nonce not found within search limit")
}
