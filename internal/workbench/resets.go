package workbench

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type resetRequest struct {
	AccountID      string `json:"accountId"`
	CreditID       string `json:"creditId,omitempty"`
	IdempotencyKey string `json:"idempotencyKey"`
}
type resetReceipt struct {
	Request resetRequest `json:"request"`
	Outcome string       `json:"outcome"`
}
type resetResponse struct {
	Attempted  bool           `json:"-"`
	Outcome    string         `json:"outcome"`
	Usage      map[string]any `json:"usage,omitempty"`
	UsageError string         `json:"usageError,omitempty"`
}

// The CLI owns redemption and idempotency. Never send a prompt to redeem a reset.
func (m *Manager) consumeReset(v resetRequest) (resetResponse, error) {
	var result resetResponse
	if strings.TrimSpace(v.AccountID) == "" || strings.TrimSpace(v.IdempotencyKey) == "" || len(v.IdempotencyKey) > 128 || len(v.CreditID) > 512 {
		return result, errors.New("accountId and a valid idempotencyKey are required")
	}
	if !m.resetMu.TryLock() {
		// This may be a retry of an unresolved request. Preserve its key until
		// the active request finishes and its receipt can be checked.
		result.Attempted = true
		return result, errors.New("another reset is in progress; retry the same attempt after it finishes")
	}
	defer m.resetMu.Unlock()
	key := fmt.Sprintf("banked-reset:%x", sha256.Sum256([]byte(v.IdempotencyKey)))
	var receipt resetReceipt
	m.store.readSetting(key, &receipt)
	if receipt.Request.IdempotencyKey != "" && receipt.Request != v {
		return result, errors.New("idempotencyKey belongs to another reset attempt")
	}
	result.Attempted = receipt.Request.IdempotencyKey != ""
	run, err := m.cli("codex")
	if err != nil {
		return result, err
	}
	_, call, close, err := probe(run, "codex", os.TempDir())
	if err != nil {
		return result, err
	}
	defer close()
	if _, err = call("initialize", map[string]any{"clientInfo": map[string]any{"name": "workbench_reset", "version": "0.1.0"}, "capabilities": map[string]any{"experimentalApi": true}}); err != nil {
		return result, err
	}
	usage, err := call("account/rateLimits/read", map[string]any{})
	if err != nil {
		return result, err
	}
	if str(usage["accountId"]) != v.AccountID {
		return result, errors.New("CLI account changed; refresh Usage and confirm the account again")
	}
	// A receipt is authoritative even when the consumed credit has disappeared.
	if receipt.Outcome != "" {
		result.Outcome = receipt.Outcome
		result.Usage = usage
		return result, nil
	}
	// Only preflight a new attempt. After an ambiguous error the same key must
	// reach the CLI again, even if the first call has already consumed the credit.
	if receipt.Request.IdempotencyKey == "" {
		summary := obj(usage["rateLimitResetCredits"])
		if summary == nil {
			return result, errors.New("banked reset information is unavailable; refresh Usage")
		}
		if v.CreditID != "" {
			found := false
			for _, raw := range asList(summary["credits"]) {
				c := obj(raw)
				if str(c["id"]) != v.CreditID {
					continue
				}
				found = true
				if str(c["status"]) != "available" || str(c["resetType"]) != "codexRateLimits" {
					return result, errors.New("reset is not available or its type is unsupported")
				}
				if _, present := c["expiresAt"]; !present {
					return result, errors.New("reset expiry is unavailable; refresh Usage")
				}
				if c["expiresAt"] != nil {
					expires, ok := c["expiresAt"].(float64)
					if !ok || expires <= float64(time.Now().Unix()) {
						return result, errors.New("reset has expired or its expiry is unavailable")
					}
				}
			}
			if !found {
				return result, errors.New("reset no longer available; refresh Usage")
			}
		} else if count, ok := summary["availableCount"].(float64); !ok || count <= 0 {
			return result, errors.New("no reset is available")
		}
		receipt.Request = v
		if err = m.store.setting(key, receipt); err != nil {
			return result, err
		}
	}
	params := map[string]any{"idempotencyKey": v.IdempotencyKey}
	if v.CreditID != "" {
		params["creditId"] = v.CreditID
	}
	result.Attempted = true
	redeemed, err := call("account/rateLimitResetCredit/consume", params)
	// Even ambiguous failures may have changed the account. Drop both caches.
	m.metaMu.Lock()
	for k := range m.metadata {
		if strings.HasPrefix(k, "codex:") {
			delete(m.metadata, k)
		}
	}
	m.metaMu.Unlock()
	if err != nil {
		return result, err
	}
	result.Outcome = str(redeemed["outcome"])
	switch result.Outcome {
	case "reset", "alreadyRedeemed", "nothingToReset", "noCredit":
	default:
		return result, errors.New("unrecognized reset result; retry only the same attempt")
	}
	receipt.Outcome = result.Outcome
	if err = m.store.setting(key, receipt); err != nil {
		result.UsageError = "Reset result received, but local receipt could not be saved: " + err.Error()
	}
	result.Usage, err = call("account/rateLimits/read", map[string]any{})
	if err != nil {
		result.UsageError = "Reset result received; usage refresh failed: " + err.Error()
	}
	return result, nil
}
