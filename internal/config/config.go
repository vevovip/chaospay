// Package config — настройки мока из ENV.
package config

import (
	"log"
	"os"
	"strconv"
)

// Config — все ENV-настройки.
type Config struct {
	// ----- Freedom Pay -----
	MerchantID     uint
	Secret         string
	TerminalID     int
	PayWebhookURL  string
	CardWebhookURL string
	AutoWebhook    bool
	HostedFormURL  string

	// ----- QR -----
	QRWebhookURL string

	// ----- Halyk Epay v2 -----
	EpayClientID          string
	EpayClientSecret      string
	EpayTerminalUUID      string
	EpaySuccessWebhookURL string
	EpayFailureWebhookURL string
	EpayBindWebhookURL    string
	EpayAutoWebhook       bool

	// ----- Flitt -----
	FlittMerchantID        int
	FlittSecret            string
	FlittSuccessWebhookURL string
	FlittBindWebhookURL    string
	FlittHostedFormURL     string
	FlittAutoWebhook       bool

	// ----- KaspiPay (polling-based) -----
	KaspiStatusPollingInterval      int
	KaspiLinkActivationWaitTimeout  int
	KaspiPaymentConfirmationTimeout int

	// ----- Loyalty (mock /loyaltyservice/loyalty/frhcCompanyTransaction) -----
	LoyaltyCashbackPercent float32
	LoyaltyCashbackBalance float32

	// ----- Общее -----
	GlobalDelaySeconds int
	ListenAddr         string
}

// Load читает Config из ENV.
func Load() Config { //nolint:funlen
	merchantID, _ := strconv.ParseUint(envOrDefault("CHAOSPAY_FREEDOM_MERCHANT_ID", "100001"), 10, 64)
	terminalID, _ := strconv.Atoi(envOrDefault("CHAOSPAY_FREEDOM_TERMINAL_ID", "1"))
	autoWebhook, _ := strconv.ParseBool(envOrDefault("CHAOSPAY_FREEDOM_AUTO_WEBHOOK", "false"))
	epayAutoWebhook, _ := strconv.ParseBool(envOrDefault("CHAOSPAY_EPAY_AUTO_WEBHOOK", "false"))
	flittAutoWebhook, _ := strconv.ParseBool(envOrDefault("CHAOSPAY_FLITT_AUTO_WEBHOOK", "true"))
	flittMerchantID, _ := strconv.Atoi(envOrDefault("CHAOSPAY_FLITT_MERCHANT_ID", "1549901"))
	delay, _ := strconv.Atoi(envOrDefault("CHAOSPAY_DELAY_SECONDS", "0"))
	loyaltyPercent, _ := strconv.ParseFloat(envOrDefault("CHAOSPAY_LOYALTY_CASHBACK_PERCENT", "10"), 32)
	loyaltyBalance, _ := strconv.ParseFloat(envOrDefault("CHAOSPAY_LOYALTY_CASHBACK_BALANCE", "10000"), 32)
	// Быстрый поллинг для e2e (реальный Kaspi обычно отдаёт 3с).
	kaspiPollInterval, _ := strconv.Atoi(envOrDefault("CHAOSPAY_KASPI_POLLING_INTERVAL", "1"))
	kaspiLinkTimeout, _ := strconv.Atoi(envOrDefault("CHAOSPAY_KASPI_LINK_TIMEOUT", "60"))
	kaspiConfirmTimeout, _ := strconv.Atoi(envOrDefault("CHAOSPAY_KASPI_CONFIRM_TIMEOUT", "120"))

	cfg := Config{
		MerchantID: uint(merchantID),
		Secret:     envOrDefault("CHAOSPAY_FREEDOM_SECRET", "mock-secret-key"),
		TerminalID: terminalID,
		// PG_* ENV-имена — про целевой PG-сервис, а не про ChaosPay → оставляем как есть.
		PayWebhookURL:  envOrDefault("PG_FREEDOM_PAY_WEBHOOK_URL", "http://payment-gateway-go-nginx:80/api/v1/payment-gateway/webhook/freedompay"),
		CardWebhookURL: envOrDefault("PG_FREEDOM_PAY_CARD_WEBHOOK_URL", "http://payment-gateway-go-nginx:80/api/v1/payment-gateway/webhook/freedompay/card"),
		AutoWebhook:    autoWebhook,
		HostedFormURL:  envOrDefault("CHAOSPAY_FREEDOM_HOSTED_URL", "http://localhost:48532/panel?bank=freedom&tab=cards"),
		QRWebhookURL:   envOrDefault("PG_WEBHOOK_URL", "http://payment-gateway-go-nginx:80/api/v1/payment-gateway/webhook/freedom-qr"),

		EpayClientID:          envOrDefault("CHAOSPAY_EPAY_CLIENT_ID", "test"),
		EpayClientSecret:      envOrDefault("CHAOSPAY_EPAY_CLIENT_SECRET", "yF587AV9Ms94qN2QShFzVR3vFnWkhjbAK3sG"),
		EpayTerminalUUID:      envOrDefault("CHAOSPAY_EPAY_TERMINAL_UUID", "67e34d63-102f-4bd1-898e-370781d0074d"),
		EpaySuccessWebhookURL: envOrDefault("PG_EPAY_POSTLINK_URL", "http://payment-gateway-go-nginx:80/api/v1/payment-gateway/webhook/epay_v2/postlink"),
		EpayFailureWebhookURL: envOrDefault("PG_EPAY_FAILURE_POSTLINK_URL", "http://payment-gateway-go-nginx:80/api/v1/payment-gateway/webhook/epay_v2/failure_postlink"),
		EpayBindWebhookURL:    envOrDefault("PG_EPAY_BIND_POSTLINK_URL", "http://payment-gateway-go-nginx:80/api/v1/payment-gateway/webhook/epay/postlink/bind"),
		EpayAutoWebhook:       epayAutoWebhook,

		FlittMerchantID:        flittMerchantID,
		FlittSecret:            envOrDefault("CHAOSPAY_FLITT_SECRET", "test"),
		FlittSuccessWebhookURL: envOrDefault("PG_FLITT_WEBHOOK_URL", "http://payment-gateway-go-nginx:80/api/v1/payment-gateway/webhook/flitt"),
		FlittBindWebhookURL:    envOrDefault("PG_FLITT_BIND_WEBHOOK_URL", "http://payment-gateway-go-nginx:80/api/v1/payment-gateway/webhook/flitt/bind"),
		FlittHostedFormURL:     envOrDefault("CHAOSPAY_FLITT_HOSTED_URL", "http://localhost:48532/panel?bank=flitt&tab=cards"),
		FlittAutoWebhook:       flittAutoWebhook,

		KaspiStatusPollingInterval:      kaspiPollInterval,
		KaspiLinkActivationWaitTimeout:  kaspiLinkTimeout,
		KaspiPaymentConfirmationTimeout: kaspiConfirmTimeout,

		LoyaltyCashbackPercent: float32(loyaltyPercent),
		LoyaltyCashbackBalance: float32(loyaltyBalance),
		GlobalDelaySeconds:     delay,
		ListenAddr:             envOrDefault("CHAOSPAY_LISTEN_ADDR", ":8532"),
	}

	log.Printf("[CONFIG] freedom: merchant=%d terminal=%d auto_webhook=%v", cfg.MerchantID, cfg.TerminalID, cfg.AutoWebhook)
	log.Printf("[CONFIG] epay: client_id=%s terminal=%s auto_webhook=%v", cfg.EpayClientID, cfg.EpayTerminalUUID, cfg.EpayAutoWebhook)
	log.Printf("[CONFIG] webhooks: pay=%s qr=%s epay_ok=%s", cfg.PayWebhookURL, cfg.QRWebhookURL, cfg.EpaySuccessWebhookURL)
	return cfg
}

// MaskSecret для UI Settings — показывает первые/последние 2 символа.
func MaskSecret(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + "****" + s[len(s)-2:]
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
