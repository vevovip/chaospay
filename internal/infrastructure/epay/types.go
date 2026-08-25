// Package epay содержит DTO, OAuth-стор и каталог ошибок Halyk Epay v2.
//
// Контракт API повторяет real Halyk Epay — структуры запросов/ответов 1-в-1
// совпадают с тем, что отправляет/ожидает PG-клиент в payment-gateway
// (см. internal/infrastructure/clients/payments/epay_2/ на стороне PG).
//
// Этот пакет — чистый infrastructure: парсит/рендерит JSON, выдаёт токены,
// маппит коды ошибок. Бизнес-логики платежа здесь нет — она в application/pay.
package epay

// TokenRequest — POST /oauth2/token. Real Halyk допускает form-urlencoded и JSON.
// Мы принимаем оба (см. handler).
type TokenRequest struct {
	GrantType    string `json:"grant_type"`
	Scope        string `json:"scope"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	InvoiceID    string `json:"invoiceID"` // именно invoiceID, не invoice_id (как в real API)
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	Terminal     string `json:"terminal"`
	SecretHash   string `json:"secret_hash"`
}

// TokenResponse — успешный ответ /oauth2/token.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    string `json:"expires_in"` // строка — так в real API
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"` // всегда "Bearer"
}

// CardID — обёртка для cardId в authorize-запросе сохранённой картой.
type CardID struct {
	ID string `json:"id"`
}

// AuthorizeRequest — POST /api/payment/cryptopay (новая карта/Apple Pay)
// и POST /api/payments/cards/auth (сохранённая карта).
type AuthorizeRequest struct {
	Amount              int     `json:"amount"`
	TerminalID          string  `json:"terminalId,omitempty"`
	InvoiceID           string  `json:"invoiceId,omitempty"`
	Currency            string  `json:"currency,omitempty"`
	Name                string  `json:"name,omitempty"`
	Cryptogram          string  `json:"cryptogram,omitempty"`         // base64(RSA(cardJSON))
	CryptogramApplePay  string  `json:"cryptogramApplePay,omitempty"` // base64 Apple Pay token
	ECI                 string  `json:"eci,omitempty"`                // 3D-Secure ECI indicator
	Description         string  `json:"description,omitempty"`
	CardID              *CardID `json:"cardId,omitempty"`
	AccountID           string  `json:"accountId,omitempty"`
	PaymentType         string  `json:"paymentType,omitempty"` // "cardId" / "applePay"
	Postlink            string  `json:"postlink,omitempty"`
	CryptogramGooglePay string  `json:"cryptogramGooglePay,omitempty"`
	FailurePostlink     string  `json:"failurePostlink,omitempty"`
	Backlink            string  `json:"backlink,omitempty"`
	FailureBacklink     string  `json:"failureBacklink,omitempty"`
	Email               string  `json:"email,omitempty"`
	Phone               string  `json:"phone,omitempty"`
	CardSave            bool    `json:"cardSave,omitempty"` // true → платёж + привязка карты
}

// Secure3D — блок 3D-Secure в AuthorizeResponse. Если null — 3DS не требуется.
type Secure3D struct {
	PaReq  string `json:"paReq"`
	MD     string `json:"md"`
	Action string `json:"action"` // ACS URL для редиректа
}

// AuthorizeResponse — ответ на cryptopay / cards/auth.
type AuthorizeResponse struct {
	ID           string    `json:"id"` // UUID операции
	Amount       int       `json:"amount"`
	AmountBonus  int       `json:"amountBonus"`
	Currency     string    `json:"currency"`
	InvoiceID    string    `json:"invoiceId"`
	AccountID    string    `json:"accountId,omitempty"`
	Email        string    `json:"email,omitempty"`
	Phone        string    `json:"phone,omitempty"`
	Description  string    `json:"description,omitempty"`
	Reference    string    `json:"reference"`    // RRN (12 знаков)
	IntReference string    `json:"intReference"` // internal Halyk reference
	Language     string    `json:"language,omitempty"`
	Secure3D     *Secure3D `json:"secure3D,omitempty"` // nil = успешная авторизация без челленджа
	CardID       string    `json:"cardID,omitempty"`
	Fee          int       `json:"fee"`
	IP           string    `json:"ip,omitempty"`
	IPCity       string    `json:"ipCity,omitempty"`
	IPCountry    string    `json:"ipCountry,omitempty"`
	IPDistrict   string    `json:"ipDistrict,omitempty"`
	IPLatitude   float64   `json:"ipLatitude,omitempty"`
	IPLongitude  float64   `json:"ipLongitude,omitempty"`
	IPRegion     string    `json:"ipRegion,omitempty"`
	Issuer       string    `json:"issuer,omitempty"`
	CardMask     string    `json:"cardMask,omitempty"`
	CardType     string    `json:"cardType,omitempty"`
	Name         string    `json:"name,omitempty"`
	Terminal     string    `json:"terminal,omitempty"`
}

// OperationResponse — ответ на charge / cancel / refund.
type OperationResponse struct {
	Code       int    `json:"code"`
	Message    string `json:"message"`
	ExternalID string `json:"externalID,omitempty"`
}

// StatusResponse — ответ Halyk `GET /check-status/payment/transactionId/{id}`.
// Используется reconciler-ом PG для проверки состояния "potentially-ambiguous"
// операций (наш аналог Freedom EX-1001 для Halyk).
//
// Реальные значения Status: AUTH (захолдировано), CHARGE (списано), CANCEL,
// REFUND, FAILED. Мок мапит наш domain.Status в эти строки.
type StatusResponse struct {
	ID           string `json:"id"`
	InvoiceID    string `json:"invoiceId"`
	Amount       int    `json:"amount"`
	Currency     string `json:"currency"`
	Status       string `json:"status"`     // AUTH | CHARGE | CANCEL | REFUND | FAILED
	StatusName   string `json:"statusName"` // human-readable
	Reference    string `json:"reference"`
	IntReference string `json:"intReference"`
	DateTime     string `json:"dateTime"`
	CardMask     string `json:"cardMask,omitempty"`
	Issuer       string `json:"issuer,omitempty"`
	Reason       string `json:"reason,omitempty"`
	ReasonCode   int    `json:"reasonCode,omitempty"`
}

// ConfirmRequest — POST /api/payment/confirm тело: результат проверки 3DS.
type ConfirmRequest struct {
	ID    string `json:"ID"`
	PaRes string `json:"PaRes"`
	MD    string `json:"MD"`
}

// ChargeRequest — POST /api/operation/{id}/charge тело.
type ChargeRequest struct {
	Amount int `json:"amount"`
}

// ErrorResponse — ошибка API (400/401/422).
type ErrorResponse struct {
	Code       int    `json:"code,omitempty"`
	Message    string `json:"message"`
	ResultCode int    `json:"resultCode,omitempty"`
}

// PostlinkPayload — успешный/неуспешный postlink, отправляемый ChaosPay → PG.
// Структура совпадает с тем, что PG ожидает в webhook epay/postlink и failure_postlink.
type PostlinkPayload struct {
	ID             string `json:"id,omitempty"`
	AccountID      string `json:"accountId,omitempty"`
	DateTime       string `json:"dateTime,omitempty"`
	InvoiceID      string `json:"invoiceId,omitempty"`
	Amount         int    `json:"amount,omitempty"`
	Currency       string `json:"currency,omitempty"`
	Terminal       string `json:"terminal,omitempty"`
	Description    string `json:"description,omitempty"`
	CardMask       string `json:"cardMask,omitempty"`
	CardType       string `json:"cardType,omitempty"`
	CardID         string `json:"cardID,omitempty"`
	Issuer         string `json:"issuer,omitempty"`
	Reference      string `json:"reference,omitempty"`
	Secure         string `json:"secure,omitempty"`
	TokenRecipient string `json:"tokenRecipient,omitempty"`
	Code           string `json:"code,omitempty"`       // "ok" / "error"
	Reason         string `json:"reason,omitempty"`     // "success" / "failure reason"
	ReasonCode     int    `json:"reasonCode,omitempty"` // 0 = success, иначе Halyk-код ошибки
	Name           string `json:"name,omitempty"`
	Email          string `json:"email,omitempty"`
}

// BindPostlinkPayload — postlink результата привязки карты (cardId / cardMask обязательны).
// Структура совпадает с request_bind.go на стороне PG.
type BindPostlinkPayload struct {
	AccountID    string  `json:"accountId,omitempty"`
	Amount       int     `json:"amount"`
	ApprovalCode string  `json:"approvalCode,omitempty"`
	CardID       string  `json:"cardId"`
	CardMask     string  `json:"cardMask"`
	CardType     string  `json:"cardType,omitempty"`
	Code         string  `json:"code"`
	Currency     string  `json:"currency,omitempty"`
	DateTime     string  `json:"dateTime,omitempty"`
	Description  string  `json:"description,omitempty"`
	Email        string  `json:"email,omitempty"`
	ID           string  `json:"id,omitempty"`
	InvoiceID    string  `json:"invoiceId"`
	IP           string  `json:"ip,omitempty"`
	IPCity       string  `json:"ipCity,omitempty"`
	IPCountry    string  `json:"ipCountry,omitempty"`
	IPDistrict   string  `json:"ipDistrict,omitempty"`
	IPLatitude   float64 `json:"ipLatitude,omitempty"`
	IPLongitude  float64 `json:"ipLongitude,omitempty"`
	IPRegion     string  `json:"ipRegion,omitempty"`
	Issuer       string  `json:"issuer,omitempty"`
	Language     string  `json:"language,omitempty"`
	Name         string  `json:"name,omitempty"`
	Phone        string  `json:"phone,omitempty"`
	Reason       string  `json:"reason,omitempty"`
	ReasonCode   int     `json:"reasonCode"`
	Reference    string  `json:"reference,omitempty"`
	Secure       string  `json:"secure,omitempty"`
	Terminal     string  `json:"terminal,omitempty"`
}
