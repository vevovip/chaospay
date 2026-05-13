// Package loyalty — Loyalty endpoints (mock-токен + frhcCompanyTransaction).
//
// frhcCompanyTransaction отвечает в схеме, которую парсит choco-freedom-loyalty
// (см. pkg/freedom-client/response.go того сервиса): поля phone, cashbackAmount,
// cashbackPercent, cashbackBalance, comment. cashbackAmount считается как процент
// от amount в теле запроса; процент и balance параметризуются ENV.
package loyalty

import (
	"encoding/json"
	"net/http"
	"time"
)

const (
	mockAccessToken = "mock-loyalty-access-token"
	mockTokenType   = "Bearer"
	mockExpiresIn   = 3600
	mockComment     = "mock chaospay"
)

// Controller — HTTP-контроллер Loyalty.
type Controller struct {
	globalDelaySeconds int
	cashbackPercent    float32
	cashbackBalance    float32
}

// NewController конструктор. cashbackPercent — процент кешбека для расчёта
// от amount (например 10 → 10%). cashbackBalance — фиксированный остаток баланса
// клиента во Freedom, который вернётся в ответе.
func NewController(globalDelaySeconds int, cashbackPercent, cashbackBalance float32) *Controller {
	return &Controller{
		globalDelaySeconds: globalDelaySeconds,
		cashbackPercent:    cashbackPercent,
		cashbackBalance:    cashbackBalance,
	}
}

// Register регистрирует loyalty routes.
func (c *Controller) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /authservice/api/auth/v1/security/getToken", c.handleGetToken)
	mux.HandleFunc("POST /loyaltyservice/loyalty/frhcCompanyTransaction", c.handleCompanyTransaction)
}

func (c *Controller) handleGetToken(w http.ResponseWriter, _ *http.Request) {
	c.delay()
	resp := map[string]any{
		"access_token": mockAccessToken,
		"token_type":   mockTokenType,
		"expires_in":   mockExpiresIn,
	}
	writeJSON(w, resp)
}

// companyTransactionRequest повторяет структуру freedom-client.Request
// (choco-freedom-loyalty/pkg/freedom-client/request.go) — это то, что мок получает.
type companyTransactionRequest struct {
	Phone         string `json:"phone"`
	Amount        int    `json:"amount"`
	CompanyName   string `json:"companyName"`
	IsTransaction int    `json:"isTransaction"`
}

// companyTransactionResponse — формат, который ждёт freedom-client.response
// (choco-freedom-loyalty/pkg/freedom-client/response.go). Имена полей менять нельзя.
type companyTransactionResponse struct {
	Phone           string  `json:"phone"`
	CashbackAmount  float32 `json:"cashbackAmount"`
	CashbackPercent float32 `json:"cashbackPercent"`
	CashbackBalance float32 `json:"cashbackBalance"`
	Comment         string  `json:"comment"`
}

func (c *Controller) handleCompanyTransaction(w http.ResponseWriter, r *http.Request) {
	c.delay()

	var req companyTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// freedom-loyalty не различает ошибки декодирования — пустой ответ
		// приводит к нулевому кешбеку, мы же хотим явный 400, чтобы было видно.
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	resp := companyTransactionResponse{
		Phone:           req.Phone,
		CashbackAmount:  float32(req.Amount) * c.cashbackPercent / 100,
		CashbackPercent: c.cashbackPercent,
		CashbackBalance: c.cashbackBalance,
		Comment:         mockComment,
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
