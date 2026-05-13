// Package config — настройки мока из ENV.
package config

import (
	"log"
	"os"
	"strconv"
)

// Config — все ENV-настройки.
type Config struct {
	// Pay
	MerchantID     uint
	Secret         string
	TerminalID     int
	PayWebhookURL  string
	CardWebhookURL string
	AutoWebhook    bool
	HostedFormURL  string

	// QR
	QRWebhookURL string

	// Loyalty (mock /loyaltyservice/loyalty/frhcCompanyTransaction)
	LoyaltyCashbackPercent float32
	LoyaltyCashbackBalance float32

	// Общее
	GlobalDelaySeconds int
	ListenAddr         string
}

// Load читает Config из ENV.
func Load() Config {
	merchantID, _ := strconv.ParseUint(envOrDefault("CHAOSPAY_FREEDOM_MERCHANT_ID", "100001"), 10, 64)
	terminalID, _ := strconv.Atoi(envOrDefault("CHAOSPAY_FREEDOM_TERMINAL_ID", "1"))
	autoWebhook, _ := strconv.ParseBool(envOrDefault("CHAOSPAY_FREEDOM_AUTO_WEBHOOK", "false"))
	delay, _ := strconv.Atoi(envOrDefault("CHAOSPAY_DELAY_SECONDS", "0"))
	loyaltyPercent, _ := strconv.ParseFloat(envOrDefault("CHAOSPAY_LOYALTY_CASHBACK_PERCENT", "10"), 32)
	loyaltyBalance, _ := strconv.ParseFloat(envOrDefault("CHAOSPAY_LOYALTY_CASHBACK_BALANCE", "10000"), 32)

	cfg := Config{
		MerchantID: uint(merchantID),
		Secret:     envOrDefault("CHAOSPAY_FREEDOM_SECRET", "mock-secret-key"),
		TerminalID: terminalID,
		// PG_* ENV-имена — про целевой PG-сервис, а не про ChaosPay → оставляем как есть.
		PayWebhookURL:      envOrDefault("PG_FREEDOM_PAY_WEBHOOK_URL", "http://payment-gateway-go-nginx:80/api/v1/payment-gateway/webhook/freedompay"),
		CardWebhookURL:     envOrDefault("PG_FREEDOM_PAY_CARD_WEBHOOK_URL", "http://payment-gateway-go-nginx:80/api/v1/payment-gateway/webhook/freedompay/card"),
		AutoWebhook:        autoWebhook,
		HostedFormURL:      envOrDefault("CHAOSPAY_FREEDOM_HOSTED_URL", "http://localhost:48532/panel?tab=cards"),
		QRWebhookURL:       envOrDefault("PG_WEBHOOK_URL", "http://payment-gateway-go-nginx:80/api/v1/payment-gateway/webhook/freedom-qr"),
		LoyaltyCashbackPercent: float32(loyaltyPercent),
		LoyaltyCashbackBalance: float32(loyaltyBalance),
		GlobalDelaySeconds:     delay,
		ListenAddr:             envOrDefault("CHAOSPAY_LISTEN_ADDR", ":8532"),
	}

	log.Printf("[CONFIG] merchant_id=%d terminal_id=%d auto_webhook=%v pay_webhook=%s qr_webhook=%s",
		cfg.MerchantID, cfg.TerminalID, cfg.AutoWebhook, cfg.PayWebhookURL, cfg.QRWebhookURL)
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
