// Package loyalty — Loyalty endpoints (mock-токен + frhcCompanyTransaction).
package loyalty

import (
	"encoding/json"
	"net/http"
	"time"
)

// Controller — HTTP-контроллер Loyalty.
type Controller struct {
	globalDelaySeconds int
}

// NewController конструктор.
func NewController(globalDelaySeconds int) *Controller {
	return &Controller{globalDelaySeconds: globalDelaySeconds}
}

// Register регистрирует loyalty routes.
func (c *Controller) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /authservice/api/auth/v1/security/getToken", c.handleGetToken)
	mux.HandleFunc("POST /loyaltyservice/loyalty/frhcCompanyTransaction", c.handleCompanyTransaction)
}

func (c *Controller) handleGetToken(w http.ResponseWriter, _ *http.Request) {
	c.delay()
	resp := map[string]any{
		"access_token": "mock-loyalty-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
	}
	writeJSON(w, resp)
}

func (c *Controller) handleCompanyTransaction(w http.ResponseWriter, _ *http.Request) {
	c.delay()
	resp := map[string]any{
		"status":      "OK",
		"description": "mock loyalty data",
		"loyaltyData": map[string]any{
			"phoneNumber":     "77770000000",
			"loyaltyBalance":  0,
			"availableAmount": 0,
		},
	}
	writeJSON(w, resp)
}

func (c *Controller) delay() {
	if c.globalDelaySeconds > 0 {
		time.Sleep(time.Duration(c.globalDelaySeconds) * time.Second)
	}
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
