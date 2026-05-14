package epay

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	domainbank "github.com/vevovip/chaospay/internal/domain/bank"
	"github.com/vevovip/chaospay/internal/domain/requestlog"
	"github.com/vevovip/chaospay/internal/domain/scenario"
	infraepay "github.com/vevovip/chaospay/internal/infrastructure/epay"
	"github.com/vevovip/chaospay/internal/ports/api/scenarioapply"
)

// handleToken — POST /oauth2/token (выдача access_token).
//
// Принимает оба формата:
//   - application/x-www-form-urlencoded (как в реальном Halyk)
//   - application/json (на случай тестового клиента)
//
// Не требует Bearer-заголовка. Сценарии применяются ТОЛЬКО transport-уровня
// (timeout/connection_reset/http_error/etc.) — content-level бессмысленны для OAuth-ответа.
func (c *Controller) handleToken(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	entry := &requestlog.Entry{
		Method:   r.Method,
		URL:      r.URL.Path,
		Endpoint: scenario.EndpointEpayToken,
		Bank:     domainbank.Epay,
	}

	bodyBytes, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	entry.RequestBody = requestlog.Truncate(string(bodyBytes), 4000)

	req := parseTokenRequest(r, bodyBytes)
	entry.OrderID = req.InvoiceID

	if c.cfg.GlobalDelaySeconds > 0 {
		time.Sleep(time.Duration(c.cfg.GlobalDelaySeconds) * time.Second)
	}

	sc := c.scenarios.Match(scenario.MatchInput{
		Bank:     domainbank.Epay,
		Endpoint: scenario.EndpointEpayToken,
		OrderID:  req.InvoiceID,
	})
	if sc != nil {
		entry.ScenarioHit = sc.ID
		entry.ScenarioName = string(sc.Action)
		if scenarioapply.Transport(w, sc, entry, started, c.log) {
			return
		}
		switch sc.Action {
		case scenario.ActionForceFailure, scenario.ActionForceUnauthorized:
			msg := scenario.Param(sc, "message", "Invalid client credentials")
			c.respondError(w, entry, started, http.StatusUnauthorized, msg)
			return
		case scenario.ActionForceForbidden:
			msg := scenario.Param(sc, "message", "Forbidden")
			c.respondError(w, entry, started, http.StatusForbidden, msg)
			return
		}
	}

	token := c.tokens.Issue(req.ClientID, req.InvoiceID, req.Amount, req.Terminal)
	resp := infraepay.TokenResponse{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresIn:    "3600",
		Scope:        "webapi usermanagement email_send verification statement statistics payment",
		TokenType:    "Bearer",
	}
	c.respondJSON(w, entry, started, http.StatusOK, resp)
}

// parseTokenRequest — принимает form-urlencoded или JSON.
// Real Halyk использует form-urlencoded, но PG-клиент может отправлять JSON.
func parseTokenRequest(r *http.Request, bodyBytes []byte) infraepay.TokenRequest {
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	var req infraepay.TokenRequest

	if strings.Contains(ct, "application/json") {
		_ = json.Unmarshal(bodyBytes, &req)
		return req
	}

	// form-urlencoded
	if err := r.ParseForm(); err != nil {
		return req
	}
	req.GrantType = r.PostFormValue("grant_type")
	req.Scope = r.PostFormValue("scope")
	req.ClientID = r.PostFormValue("client_id")
	req.ClientSecret = r.PostFormValue("client_secret")
	req.InvoiceID = r.PostFormValue("invoiceID")
	req.Amount = r.PostFormValue("amount")
	req.Currency = r.PostFormValue("currency")
	req.Terminal = r.PostFormValue("terminal")
	req.SecretHash = r.PostFormValue("secret_hash")
	return req
}
