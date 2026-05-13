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
	"github.com/vevovip/chaospay/internal/infrastructure/memstore"
	"github.com/vevovip/chaospay/internal/infrastructure/pgclient"
	"github.com/vevovip/chaospay/internal/infrastructure/qrgen"
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
	qrGenerator := qrgen.NewGenerator("")

	// Application
	payService := apppay.NewService(payRepo, payWebhookClient, cardWebhookClient, cfg.AutoWebhook)
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
	loyaltyCtrl := loyalty.NewController(cfg.GlobalDelaySeconds)
	panelCtrl := panel.NewController(payService, qrService, scenarioService, requestLog, cfg)

	mux := http.NewServeMux()
	payCtrl.Register(mux)
	walletCtrl.Register(mux)
	qrCtrl.Register(mux)
	loyaltyCtrl.Register(mux)
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
	log.Println("  Panel:")
	log.Println("    GET  /panel              (tabs: cards, qr, scenarios, log, settings)")
	log.Println("    GET  /qr-panel           (alias → /panel?tab=qr)")
	log.Printf("  QR Webhook URL:   %s", cfg.QRWebhookURL)
	log.Printf("  Pay Webhook URL:  %s", cfg.PayWebhookURL)
	log.Printf("  Card Webhook URL: %s", cfg.CardWebhookURL)
}
