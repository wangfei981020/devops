package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"opsplatform-probe-backend/database"
)

type agentCtxKey string

const (
	ctxAgentID  agentCtxKey = "agentID"
	ctxAgentDB  agentCtxKey = "agentDBID"
	ctxAgentRow agentCtxKey = "agentRow"
)

// AgentTokenMiddleware authenticates Agent calls.
// Header: Authorization: Bearer <agent-token>
// X-Agent-ID: <agent_id>
func AgentTokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			jsonError(w, http.StatusUnauthorized, "missing agent token")
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		agentID := r.Header.Get("X-Agent-ID")
		if agentID == "" {
			jsonError(w, http.StatusUnauthorized, "missing X-Agent-ID")
			return
		}

		hash := sha256.Sum256([]byte(token))
		tokenHash := hex.EncodeToString(hash[:])

		var dbID int
		var dbHash, status string
		var approved int
		var tokenExpiresAt *time.Time
		err := database.DB.QueryRow(
			"SELECT id, token_hash, status, approved, token_expires_at FROM agents WHERE agent_id = ?", agentID,
		).Scan(&dbID, &dbHash, &status, &approved, &tokenExpiresAt)
		if err != nil {
			jsonError(w, http.StatusUnauthorized, "agent not registered")
			return
		}
		if dbHash == "" || dbHash != tokenHash {
			jsonError(w, http.StatusUnauthorized, "invalid agent token")
			return
		}
		if approved == 0 {
			jsonError(w, http.StatusForbidden, "agent not approved")
			return
		}
		if status == "disabled" {
			jsonError(w, http.StatusForbidden, "agent disabled")
			return
		}
		// Token 过期检查 (NULL 表示永不过期)
		if tokenExpiresAt != nil && !tokenExpiresAt.IsZero() && time.Now().After(*tokenExpiresAt) {
			jsonError(w, http.StatusUnauthorized, "agent token expired, please reissue")
			return
		}

		ctx := context.WithValue(r.Context(), ctxAgentID, agentID)
		ctx = context.WithValue(ctx, ctxAgentDB, dbID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func currentAgentID(r *http.Request) string {
	if v := r.Context().Value(ctxAgentID); v != nil {
		return v.(string)
	}
	return ""
}
