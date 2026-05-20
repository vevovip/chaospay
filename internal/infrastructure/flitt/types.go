package flitt

import "encoding/json"

// Response-status константы Flitt.
const (
	ResponseStatusSuccess = "success"
	ResponseStatusFailure = "failure"
)

// Order-status константы Flitt (значения order_status в API/webhook).
const (
	OrderStatusApproved   = "approved"
	OrderStatusDeclined   = "declined"
	OrderStatusProcessing = "processing"
	OrderStatusReversed   = "reversed"
	OrderStatusExpired    = "expired"
	OrderStatusCreated    = "created"
)

// Capture / Reverse статусы.
const (
	CaptureStatusCaptured = "captured"
	CaptureStatusDeclined = "declined"
	ReverseStatusApproved = "approved"
	ReverseStatusDeclined = "declined"
)

// ----- /api/checkout/url -----

// CheckoutRequest — тело запроса hosted-формы. JSON ровно соответствует
// `pkg/flitt/commands/payment/command.go::Command`.
type CheckoutRequest struct {
	OrderID           string `json:"order_id"`
	Amount            int    `json:"amount"`
	Currency          string `json:"currency"`
	OrderDesc         string `json:"order_desc"`
	ResponseURL       string `json:"response_url,omitempty"`
	ServerCallbackURL string `json:"server_callback_url,omitempty"`
	SenderEmail       string `json:"sender_email,omitempty"`
	Language          string `json:"language,omitempty"`
	Lifetime          int    `json:"lifetime,omitempty"`
	MerchantData      string `json:"merchant_data,omitempty"`
	RequiredRectoken  string `json:"required_rectoken,omitempty"` // Y/N
	Preauth           string `json:"preauth,omitempty"`
	Verification      string `json:"verification,omitempty"`
	MerchantID        int    `json:"merchant_id"`
	Signature         string `json:"signature"`
}

// CheckoutResponse — успешный ответ /api/checkout/url.
type CheckoutResponse struct {
	ResponseStatus string      `json:"response_status"`
	CheckoutURL    string      `json:"checkout_url,omitempty"`
	ErrorMessage   string      `json:"error_message,omitempty"`
	ErrorCode      interface{} `json:"error_code,omitempty"`
}

// CheckoutEnvelope обёртка ответа.
type CheckoutEnvelope struct {
	Response CheckoutResponse `json:"response"`
}

// Wrapper — общий wrapper, для случаев когда тело пришло в `{"request": ...}`.
type Wrapper struct {
	Request json.RawMessage `json:"request"`
}

// ----- /api/3dsecure_step1 (direct) -----

// DirectRequest — Apple/Google Pay прямой платёж (container=base64(token)).
type DirectRequest struct {
	OrderID           string `json:"order_id"`
	MerchantID        int    `json:"merchant_id"`
	OrderDesc         string `json:"order_desc"`
	Amount            int    `json:"amount"`
	Currency          string `json:"currency"`
	Container         string `json:"container"`
	ServerCallbackURL string `json:"server_callback_url"`
	ClientIP          string `json:"client_ip,omitempty"`
	Preauth           string `json:"preauth,omitempty"`
	Signature         string `json:"signature"`
}

// DirectResponse — структура ответа на /api/3dsecure_step1.
type DirectResponse struct {
	ResponseStatus     string      `json:"response_status"`
	ACSURL             string      `json:"acs_url,omitempty"`
	Pareq              string      `json:"pareq,omitempty"`
	MD                 string      `json:"md,omitempty"`
	CheckoutURL        string      `json:"checkout_url,omitempty"`
	OrderID            string      `json:"order_id,omitempty"`
	Rectoken           string      `json:"rectoken,omitempty"`
	ResponseCode       string      `json:"response_code,omitempty"`
	RRN                string      `json:"rrn,omitempty"`
	MaskedCard         string      `json:"masked_card,omitempty"`
	SenderCell         string      `json:"sender_cell_phone,omitempty"`
	SenderAccount      string      `json:"sender_account,omitempty"`
	Fee                string      `json:"fee,omitempty"`
	ReversedAmount     string      `json:"reversed_amount,omitempty"`
	SettlementDate     string      `json:"settlement_date,omitempty"`
	SettlementAmount   string      `json:"settlement_amount,omitempty"`
	SettlementCurrency string      `json:"settlement_currency,omitempty"`
	ApprovalCode       string      `json:"approval_code,omitempty"`
	OrderStatus        string      `json:"order_status,omitempty"`
	PaymentID          interface{} `json:"payment_id,omitempty"`
	ErrorMessage       string      `json:"error_message,omitempty"`
	ErrorCode          int         `json:"error_code,omitempty"`
}

// DirectEnvelope — обёртка ответа /api/3dsecure_step1.
type DirectEnvelope struct {
	Response DirectResponse `json:"response"`
}

// ----- /api/recurring -----

// RecurringRequest — списание сохранённой картой по rectoken.
type RecurringRequest struct {
	Version           string `json:"version,omitempty"`
	OrderID           string `json:"order_id"`
	Amount            int    `json:"amount"`
	Currency          string `json:"currency"`
	OrderDesc         string `json:"order_desc"`
	Rectoken          string `json:"rectoken"`
	ServerCallbackURL string `json:"server_callback_url,omitempty"`
	Preauth           string `json:"preauth,omitempty"`
	CVV2              string `json:"cvv2,omitempty"`
	ClientIP          string `json:"client_ip,omitempty"`
	Lifetime          int    `json:"lifetime,omitempty"`
	SenderEmail       string `json:"sender_email,omitempty"`
	ProductID         string `json:"product_id,omitempty"`
	MerchantData      string `json:"merchant_data,omitempty"`
	MerchantID        int    `json:"merchant_id"`
	Signature         string `json:"signature"`
}

// RecurringResponse — ответ /api/recurring.
type RecurringResponse struct {
	ResponseStatus string      `json:"response_status"`
	OrderStatus    string      `json:"order_status,omitempty"`
	PaymentID      interface{} `json:"payment_id,omitempty"`
	MaskedCard     string      `json:"masked_card,omitempty"`
	OrderID        string      `json:"order_id,omitempty"`
	Amount         string      `json:"amount,omitempty"`
	Currency       string      `json:"currency,omitempty"`
	ApprovalCode   string      `json:"approval_code,omitempty"`
	RRN            string      `json:"rrn,omitempty"`
	CardType       string      `json:"card_type,omitempty"`
	Rectoken       string      `json:"rectoken,omitempty"`
	ErrorMessage   string      `json:"error_message,omitempty"`
	ErrorCode      interface{} `json:"error_code,omitempty"`
}

// RecurringEnvelope — обёртка ответа /api/recurring.
type RecurringEnvelope struct {
	Response RecurringResponse `json:"response"`
}

// ----- /api/capture/order_id -----

// CaptureRequest — тело /api/capture/order_id.
type CaptureRequest struct {
	Version    string `json:"version"`
	OrderID    string `json:"order_id"`
	Amount     int    `json:"amount"`
	Currency   string `json:"currency"`
	MerchantID int    `json:"merchant_id"`
	Signature  string `json:"signature"`
}

// CaptureResponse — ответ /api/capture/order_id.
type CaptureResponse struct {
	CaptureStatus       string `json:"capture_status"`
	OrderID             string `json:"order_id"`
	ResponseDescription string `json:"response_description"`
	ResponseCode        string `json:"response_code"`
	MerchantID          int    `json:"merchant_id"`
	ResponseStatus      string `json:"response_status"`
	ErrorCode           int    `json:"error_code,omitempty"`
	ErrorMessage        string `json:"error_message,omitempty"`
}

// CaptureEnvelope — обёртка ответа /api/capture/order_id.
type CaptureEnvelope struct {
	Response CaptureResponse `json:"response"`
}

// ----- /api/reverse/order_id -----

// ReverseRequest — тело /api/reverse/order_id.
type ReverseRequest struct {
	Version    string `json:"version"`
	OrderID    string `json:"order_id"`
	Amount     int    `json:"amount"`
	Currency   string `json:"currency"`
	MerchantID int    `json:"merchant_id"`
	Email      string `json:"email,omitempty"`
	Comment    string `json:"comment,omitempty"`
	ReverseID  string `json:"reverse_id,omitempty"`
	Signature  string `json:"signature"`
}

// ReverseResponse — ответ /api/reverse/order_id.
type ReverseResponse struct {
	ReverseStatus       string `json:"reverse_status"`
	OrderID             string `json:"order_id"`
	ResponseDescription string `json:"response_description"`
	ResponseCode        string `json:"response_code"`
	MerchantID          int    `json:"merchant_id"`
	ResponseStatus      string `json:"response_status"`
	Signature           string `json:"signature"`
	ReverseID           string `json:"reverse_id"`
	ReversalAmount      string `json:"reversal_amount"`
	TransactionID       string `json:"transaction_id"`
	Comment             string `json:"comment,omitempty"`
	ErrorCode           int    `json:"error_code,omitempty"`
	ErrorMessage        string `json:"error_message,omitempty"`
}

// ReverseEnvelope — обёртка ответа /api/reverse/order_id.
type ReverseEnvelope struct {
	Response ReverseResponse `json:"response"`
}

// ----- /api/status/order_id -----

// StatusRequest — тело /api/status/order_id.
type StatusRequest struct {
	Version    string `json:"version"`
	OrderID    string `json:"order_id"`
	MerchantID int    `json:"merchant_id"`
	Signature  string `json:"signature"`
}

// StatusResponse — ответ /api/status/order_id (расширенный).
type StatusResponse struct {
	RRN                 string `json:"rrn"`
	MaskedCard          string `json:"masked_card"`
	SenderCellPhone     string `json:"sender_cell_phone"`
	SenderAccount       string `json:"sender_account"`
	Currency            string `json:"currency"`
	Fee                 string `json:"fee"`
	ReversalAmount      string `json:"reversal_amount"`
	SettlementAmount    string `json:"settlement_amount"`
	ActualAmount        string `json:"actual_amount"`
	ResponseDescription string `json:"response_description"`
	SenderEmail         string `json:"sender_email"`
	OrderStatus         string `json:"order_status"`
	ResponseStatus      string `json:"response_status"`
	OrderTime           string `json:"order_time"`
	ActualCurrency      string `json:"actual_currency"`
	OrderID             string `json:"order_id"`
	TranType            string `json:"tran_type"`
	ECI                 string `json:"eci"`
	SettlementDate      string `json:"settlement_date"`
	PaymentSystem       string `json:"payment_system"`
	ApprovalCode        string `json:"approval_code"`
	MerchantID          int    `json:"merchant_id"`
	SettlementCurrency  string `json:"settlement_currency"`
	PaymentID           int64  `json:"payment_id"`
	CardBin             int    `json:"card_bin"`
	ResponseCode        string `json:"response_code"`
	CardType            string `json:"card_type"`
	Amount              string `json:"amount"`
	Signature           string `json:"signature"`
	ProductID           string `json:"product_id"`
	MerchantData        string `json:"merchant_data"`
	Rectoken            string `json:"rectoken"`
	RectokenLifetime    string `json:"rectoken_lifetime"`
	VerificationStatus  string `json:"verification_status"`
	ParentOrderID       string `json:"parent_order_id"`
	FeeOplata           string `json:"fee_oplata"`
	AdditionalInfo      string `json:"additional_info"`
	ErrorCode           int    `json:"error_code,omitempty"`
	ErrorMessage        string `json:"error_message,omitempty"`
}

// StatusEnvelope — обёртка ответа /api/status/order_id.
type StatusEnvelope struct {
	Response StatusResponse `json:"response"`
}

// ----- /api/3dsecure_step2 -----

// Step2Request — тело /api/3dsecure_step2.
type Step2Request struct {
	MerchantID string `json:"merchant_id"`
	OrderID    string `json:"order_id"`
	Pares      string `json:"pares"`
	MD         string `json:"md"`
	Version    string `json:"version,omitempty"`
	Signature  string `json:"signature"`
}

// Step2Response — ответ /api/3dsecure_step2.
type Step2Response struct {
	ResponseStatus     string      `json:"response_status"`
	OrderID            string      `json:"order_id"`
	OrderStatus        string      `json:"order_status"`
	ResponseCode       string      `json:"response_code,omitempty"`
	RRN                string      `json:"rrn,omitempty"`
	ApprovalCode       string      `json:"approval_code,omitempty"`
	MaskedCard         string      `json:"masked_card,omitempty"`
	SettlementDate     string      `json:"settlement_date,omitempty"`
	SettlementAmount   string      `json:"settlement_amount,omitempty"`
	SettlementCurrency string      `json:"settlement_currency,omitempty"`
	PaymentID          interface{} `json:"payment_id,omitempty"`
	ErrorMessage       string      `json:"error_message,omitempty"`
	ErrorCode          int         `json:"error_code,omitempty"`
	RequestID          string      `json:"request_id,omitempty"`
	Rectoken           string      `json:"rectoken,omitempty"`
}

// Step2Envelope — обёртка ответа /api/3dsecure_step2.
type Step2Envelope struct {
	Response Step2Response `json:"response"`
}

// ----- Errors -----

// ErrorResponse — generic failure-обёртка.
type ErrorResponse struct {
	Response ErrorPayload `json:"response"`
}

// ErrorPayload — тело ошибки.
type ErrorPayload struct {
	ResponseStatus string      `json:"response_status"`
	ErrorMessage   string      `json:"error_message"`
	ErrorCode      interface{} `json:"error_code"`
	RequestID      string      `json:"request_id,omitempty"`
}

// NewFailure формирует FAILURE-конверт с указанным кодом/сообщением.
func NewFailure(code int, msg string) ErrorResponse {
	return ErrorResponse{
		Response: ErrorPayload{
			ResponseStatus: ResponseStatusFailure,
			ErrorCode:      code,
			ErrorMessage:   msg,
		},
	}
}

// ----- Webhook payload (мок → PG) -----

// CallbackPayload — то, что мок шлёт на /api/v1/payment-gateway/webhook/flitt.
// Структура совпадает с тем, что Flitt отдаёт в реальном callback (см.
// `ports/api/v1/webhook/flitt/request.go` в payment-gateway-new).
type CallbackPayload struct {
	RRN                 string          `json:"rrn"`
	MaskedCard          string          `json:"masked_card"`
	SenderCellPhone     string          `json:"sender_cell_phone"`
	SenderAccount       string          `json:"sender_account"`
	Currency            string          `json:"currency"`
	Fee                 string          `json:"fee"`
	ReversalAmount      string          `json:"reversal_amount"`
	SettlementAmount    string          `json:"settlement_amount"`
	ActualAmount        string          `json:"actual_amount"`
	ResponseDescription string          `json:"response_description"`
	SenderEmail         string          `json:"sender_email"`
	OrderStatus         string          `json:"order_status"`
	ResponseStatus      string          `json:"response_status"`
	OrderTime           string          `json:"order_time"`
	ActualCurrency      string          `json:"actual_currency"`
	OrderID             string          `json:"order_id"`
	TranType            string          `json:"tran_type"`
	ECI                 string          `json:"eci"`
	SettlementDate      string          `json:"settlement_date"`
	PaymentSystem       string          `json:"payment_system"`
	ApprovalCode        string          `json:"approval_code"`
	MerchantID          int             `json:"merchant_id"`
	SettlementCurrency  string          `json:"settlement_currency"`
	PaymentID           int             `json:"payment_id"`
	CardBin             int             `json:"card_bin"`
	ResponseCode        json.RawMessage `json:"response_code,omitempty"`
	CardType            string          `json:"card_type"`
	Amount              string          `json:"amount"`
	Signature           string          `json:"signature"`
	ProductID           string          `json:"product_id"`
	MerchantData        string          `json:"merchant_data"`
	RecToken            string          `json:"rectoken,omitempty"`
	RecTokenLifeTime    string          `json:"rectoken_lifetime,omitempty"`
	VerificationStatus  string          `json:"verification_status,omitempty"`
	ParentOrderID       string          `json:"parent_order_id,omitempty"`
	FeeOplata           string          `json:"fee_oplata,omitempty"`
	AdditionalInfo      string          `json:"additional_info,omitempty"`
}
