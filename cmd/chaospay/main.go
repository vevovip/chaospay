// Mock Freedom Bank — entry point.
//
// Bootstrap собирает зависимости (DI через конструкторы), регистрирует HTTP-routes и поднимает сервер.
//
// Слои:
//
//	internal/domain          — типы и константы (без зависимостей)
//	internal/application     — оркестрация (services), зависят только от domain + интерфейсов
//	internal/infrastructure  — реализации (memstore, freedompay sign/xml, pgclient HTTP, qrgen)
//	internal/ports/api       — HTTP handlers (pay, wallet, qr, loyalty, health)
//	internal/ports/panel     — HTML панель управления
package main

import (
	"log"
	"net/http"

	apppay "github.com/vevovip/chaospay/internal/application/pay"
	appqr "github.com/vevovip/chaospay/internal/application/qr"
	appscenario "github.com/vevovip/chaospay/internal/application/scenario"
	"github.com/vevovip/chaospay/internal/config"
	infraepay "github.com/vevovip/chaospay/internal/infrastructure/epay"
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
	"github.com/vevovip/chaospay/internal/infrastructure/pgclient"
	"github.com/vevovip/chaospay/internal/infrastructure/qrgen"
	epayports "github.com/vevovip/chaospay/internal/ports/api/epay"
	flittports "github.com/vevovip/chaospay/internal/ports/api/flitt"
	"github.com/vevovip/chaospay/internal/ports/api/health"
	"github.com/vevovip/chaospay/internal/ports/api/loyalty"
	payports "github.com/vevovip/chaospay/internal/ports/api/pay"
	qrports "github.com/vevovip/chaospay/internal/ports/api/qr"
	walletports "github.com/vevovip/chaospay/internal/ports/api/wallet"
	"github.com/vevovip/chaospay/internal/ports/panel"
)

func main() {
	cfg := config.Load()

	// Infrastructure
	payRepo := memstore.NewPayRepo()
	qrRepo := memstore.NewQRRepo()
	scenarioStore := memstore.NewScenarioStore()
	requestLog := memstore.NewRequestLog(0)

	payWebhookClient := pgclient.NewPayClient(cfg.PayWebhookURL, cfg.Secret)
	cardWebhookClient := pgclient.NewCardClient(cfg.CardWebhookURL, cfg.Secret)
	qrWebhookClient := pgclient.NewQRClient(cfg.QRWebhookURL)
	epayWebhookClient := pgclient.NewEpayClient(cfg.EpaySuccessWebhookURL, cfg.EpayFailureWebhookURL, cfg.EpayBindWebhookURL)
	epayTokens := infraepay.NewTokenStore()
	qrGenerator := qrgen.NewGenerator("")

	// Application
	flittWebhookClient := pgclient.NewFlittClient(cfg.FlittSuccessWebhookURL, cfg.FlittBindWebhookURL, cfg.FlittSecret)
	payService := apppay.NewService(
		payRepo, payWebhookClient, cardWebhookClient, epayWebhookClient, flittWebhookClient,
		apppay.AutoWebhookConfig{
			Freedom: cfg.AutoWebhook,
			Epay:    cfg.EpayAutoWebhook,
			Flitt:   cfg.FlittAutoWebhook,
		},
	)
	qrService := appqr.NewService(qrRepo, qrGenerator, qrgen.GenerateUUID, qrWebhookClient)
	scenarioService := appscenario.NewService(scenarioStore)

	// Ports / API
	payCtrl := payports.NewController(payService, scenarioService, requestLog, payports.Config{
		Secret:             cfg.Secret,
		DefaultTerminalID:  cfg.TerminalID,
		HostedFormURL:      cfg.HostedFormURL,
		GlobalDelaySeconds: cfg.GlobalDelaySeconds,
	})
	walletCtrl := walletports.NewController(payService, scenarioService, requestLog)
	qrCtrl := qrports.NewController(qrService, cfg.GlobalDelaySeconds)
	loyaltyCtrl := loyalty.NewController(cfg.GlobalDelaySeconds, cfg.LoyaltyCashbackPercent, cfg.LoyaltyCashbackBalance)
	epayCtrl := epayports.NewController(payService, scenarioService, requestLog, epayTokens, epayWebhookClient, epayports.Config{
		Creds:              map[string]string{cfg.EpayClientID: cfg.EpayClientSecret},
		TerminalUUID:       cfg.EpayTerminalUUID,
		AutoWebhook:        cfg.EpayAutoWebhook,
		GlobalDelaySeconds: cfg.GlobalDelaySeconds,
	})
	flittCtrl := flittports.NewController(payService, scenarioService, requestLog, flittWebhookClient, flittports.Config{
		Secret:             cfg.FlittSecret,
		MerchantID:         cfg.FlittMerchantID,
		HostedFormURL:      cfg.FlittHostedFormURL,
		AutoWebhook:        cfg.FlittAutoWebhook,
		GlobalDelaySeconds: cfg.GlobalDelaySeconds,
	})
	panelCtrl := panel.NewController(payService, qrService, scenarioService, requestLog, cfg)

	mux := http.NewServeMux()
	payCtrl.Register(mux)
	walletCtrl.Register(mux)
	qrCtrl.Register(mux)
	loyaltyCtrl.Register(mux)
	epayCtrl.Register(mux)
	flittCtrl.Register(mux)
	panelCtrl.Register(mux)
	health.Register(mux)

	logRoutes(cfg)

	log.Printf("[mock] listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil { //nolint:gosec
		log.Fatal(err)
	}
}

func logRoutes(cfg config.Config) {
	log.Println("[mock] routes:")
	log.Println("  Loyalty:")
	log.Println("    POST /authservice/api/auth/v1/security/getToken")
	log.Println("    POST /loyaltyservice/loyalty/frhcCompanyTransaction")
	log.Println("  QR-PAY:")
	log.Println("    POST /qr-code/generate (dataType=001 — payment, dataType=003 — refund)")
	log.Println("    GET  /qr-code/get-status/{uuid}")
	log.Println("    POST /qr-code/change-status")
	log.Println("    GET  /qr-code/get-status-refund/{uuid}")
	log.Println("    POST /qr-code/confirm-refund")
	log.Println("  Freedom Pay XML:")
	log.Println("    POST /v1/merchant/{id}/card/init    (HoldInit)")
	log.Println("    POST /v1/merchant/{id}/card/direct  (Hold)")
	log.Println("    POST /get_status3.php               (Status)")
	log.Println("    POST /do_capture.php                (Capture)")
	log.Println("    POST /cancel.php                    (Cancel)")
	log.Println("    POST /revoke.php                    (Revoke)")
	log.Println("    POST /init_payment.php              (PayPage)")
	log.Println("    POST /v1/merchant/{id}/cardstorage/add2   (AddCard)")
	log.Println("    POST /v1/merchant/{id}/cardstorage/remove (RemoveCard)")
	log.Println("    POST /pay/{paymentID}/pay           (ApplePay JSON / GooglePay form)")
	log.Println("  Flitt JSON:")
	log.Println("    POST /api/checkout/url               (Hosted-форма)")
	log.Println("    POST /api/3dsecure_step1             (Direct: Apple/Google Pay)")
	log.Println("    POST /api/recurring                  (Сохранённая карта)")
	log.Println("    POST /api/capture/order_id           (Capture)")
	log.Println("    POST /api/reverse/order_id           (Reverse / Refund)")
	log.Println("    POST /api/status/order_id            (Status)")
	log.Println("    POST /api/3dsecure_step2             (3DS Step 2)")
	log.Println("  Halyk Epay v2 JSON:")
	log.Println("    POST /oauth2/token                          (OAuth — выдача access_token)")
	log.Println("    POST /api/payment/cryptopay                 (Cryptopay: новая карта / ApplePay)")
	log.Println("    POST /api/payments/cards/auth               (Сохранённая карта)")
	log.Println("    POST /api/operation/{id}/charge             (Charge)")
	log.Println("    POST /api/operation/{id}/cancel             (Cancel)")
	log.Println("    POST /api/operation/{id}/refund?amount=…    (Refund)")
	log.Println("  Panel:")
	log.Println("    GET  /panel?bank=<freedom|epay|qr|loyalty>&tab=<cards|scenarios|log>")
	log.Println("    GET  /qr-panel           (alias → /panel?bank=qr&tab=qr)")
	log.Printf("  QR Webhook URL:        %s", cfg.QRWebhookURL)
	log.Printf("  Pay Webhook URL:       %s", cfg.PayWebhookURL)
	log.Printf("  Card Webhook URL:      %s", cfg.CardWebhookURL)
	log.Printf("  Epay Postlink URL:     %s", cfg.EpaySuccessWebhookURL)
	log.Printf("  Epay Failure URL:      %s", cfg.EpayFailureWebhookURL)
	log.Printf("  Epay Bind Postlink URL:%s", cfg.EpayBindWebhookURL)
	log.Printf("  Flitt Webhook URL:     %s", cfg.FlittSuccessWebhookURL)
	log.Printf("  Flitt Bind Webhook URL:%s", cfg.FlittBindWebhookURL)
}
