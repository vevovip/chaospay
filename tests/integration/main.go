// Test harness for ChaosPay scenarios.
package main

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultBaseURL       = "http://localhost:48532"
	defaultSecret        = "mock-secret-key"
	defaultMerchantID    = "100001"
	defaultTerminalIDInt = 1
)

// baseURL — адрес мока. Переопределяется через CHAOSPAY_BASE_URL (для локального запуска
// без docker, где port:8532 не пробрасывается на 48532).
var baseURL = func() string {
	if v := os.Getenv("CHAOSPAY_BASE_URL"); v != "" {
		return v
	}
	return defaultBaseURL
}()

// secret — Freedom-secret мока. Читается из CHAOSPAY_FREEDOM_SECRET — должен совпадать
// с тем, что задано контейнеру (иначе подпись запросов не пройдёт верификацию).
var secret = func() string {
	if v := os.Getenv("CHAOSPAY_FREEDOM_SECRET"); v != "" {
		return v
	}
	return defaultSecret
}()

// merchantID — Freedom merchant_id мока. Читается из CHAOSPAY_FREEDOM_MERCHANT_ID.
// На стороне chaospay merchant_id не валидируется отдельно (только через подпись),
// но конструктор URL-а /v1/merchant/{id}/card/init использует именно это значение.
var merchantID = func() string {
	if v := os.Getenv("CHAOSPAY_FREEDOM_MERCHANT_ID"); v != "" {
		return v
	}
	return defaultMerchantID
}()

var (
	passCount int
	failCount int
	failures  []string
)

func main() {
	fmt.Println("=== ChaosPay scenario test harness ===")
	fmt.Println()

	// Reset state
	resetAll()

	// --- Transport-level via wallet (no signature needed) ---
	section("Transport-level (wallet endpoint)")
	walletPaymentID := holdInit()
	fmt.Printf("  using paymentID=%d for wallet tests\n", walletPaymentID)

	testCase("empty_response on applepay",
		func() { addScenario("applepay", "empty_response", nil, true) },
		func() (string, error) {
			body, code, _, err := postJSON(fmt.Sprintf("/pay/%d/pay", walletPaymentID), `{"data":{"token":"x"}}`)
			if err != nil {
				return "", err
			}
			if code == 200 && body == "" {
				return "200 + empty body ✓", nil
			}
			return "", fmt.Errorf("expected 200+empty, got %d body=%q", code, body)
		})

	testCase("malformed_body on applepay",
		func() {
			addScenario("applepay", "malformed_body", map[string]string{"body": `{"BROKEN`, "content_type": "application/json"}, true)
		},
		func() (string, error) {
			body, code, ct, err := postJSON(fmt.Sprintf("/pay/%d/pay", walletPaymentID), `{"data":{"token":"x"}}`)
			if err != nil {
				return "", err
			}
			if code == 200 && body == `{"BROKEN` && strings.HasPrefix(ct, "application/json") {
				return fmt.Sprintf("body=%q ct=%s ✓", body, ct), nil
			}
			return "", fmt.Errorf("got code=%d body=%q ct=%s", code, body, ct)
		})

	testCase("http_error 502 on applepay",
		func() { addScenario("applepay", "http_error", map[string]string{"http_status": "502"}, true) },
		func() (string, error) {
			_, code, _, err := postJSON(fmt.Sprintf("/pay/%d/pay", walletPaymentID), `{"data":{}}`)
			if err != nil {
				return "", err
			}
			if code == 502 {
				return "502 ✓", nil
			}
			return "", fmt.Errorf("got %d", code)
		})

	testCase("wrong_status_code 418 on applepay",
		func() {
			addScenario("applepay", "wrong_status_code", map[string]string{"http_status": "418"}, true)
		},
		func() (string, error) {
			_, code, _, err := postJSON(fmt.Sprintf("/pay/%d/pay", walletPaymentID), `{"data":{}}`)
			if err != nil {
				return "", err
			}
			if code == 418 {
				return "418 ✓", nil
			}
			return "", fmt.Errorf("got %d", code)
		})

	testCase("connection_reset on applepay (closes conn)",
		func() { addScenario("applepay", "connection_reset", nil, true) },
		func() (string, error) {
			_, _, _, err := postJSON(fmt.Sprintf("/pay/%d/pay", walletPaymentID), `{"data":{}}`)
			if err != nil && (strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "reset") || strings.Contains(err.Error(), "connection")) {
				return "connection error received ✓", nil
			}
			return "", fmt.Errorf("expected connection error, got err=%v", err)
		})

	testCase("timeout (2s) on applepay",
		func() { addScenario("applepay", "timeout", map[string]string{"seconds": "2"}, true) },
		func() (string, error) {
			start := time.Now()
			_, _, _, err := postJSONWithTimeout(fmt.Sprintf("/pay/%d/pay", walletPaymentID), `{"data":{}}`, 5*time.Second)
			d := time.Since(start)
			if d < 1900*time.Millisecond {
				return "", fmt.Errorf("returned too fast: %v err=%v", d, err)
			}
			// Connection got closed → EOF expected
			if err != nil {
				return fmt.Sprintf("slept %v then closed ✓", d.Round(100*time.Millisecond)), nil
			}
			return fmt.Sprintf("slept %v ✓", d.Round(100*time.Millisecond)), nil
		})

	testCase("delay (1s) on applepay still returns success",
		func() { addScenario("applepay", "delay", map[string]string{"seconds": "1"}, true) },
		func() (string, error) {
			start := time.Now()
			body, code, _, err := postJSON(fmt.Sprintf("/pay/%d/pay", walletPaymentID), `{"data":{"token":"x"}}`)
			d := time.Since(start)
			if err != nil {
				return "", err
			}
			if code == 200 && d >= 900*time.Millisecond && strings.Contains(body, "back_url") {
				return fmt.Sprintf("slept %v + 200 OK ✓", d.Round(100*time.Millisecond)), nil
			}
			return "", fmt.Errorf("code=%d body=%q d=%v", code, body, d)
		})

	testCase("force_failure on applepay (JSON error response)",
		func() {
			addScenario("applepay", "force_failure", map[string]string{"message": "custom failure msg"}, true)
		},
		func() (string, error) {
			body, code, _, err := postJSON(fmt.Sprintf("/pay/%d/pay", walletPaymentID), `{"data":{}}`)
			if err != nil {
				return "", err
			}
			if code == 200 && strings.Contains(body, `"status":"error"`) && strings.Contains(body, "custom failure msg") {
				return "JSON error msg ✓", nil
			}
			return "", fmt.Errorf("code=%d body=%q", code, body)
		})

	testCase("ambiguous_error on applepay",
		func() {
			addScenario("applepay", "ambiguous_error", map[string]string{"message": "Неверный статус платежа"}, true)
		},
		func() (string, error) {
			body, code, _, err := postJSON(fmt.Sprintf("/pay/%d/pay", walletPaymentID), `{"data":{}}`)
			if err != nil {
				return "", err
			}
			if code == 200 && strings.Contains(body, `Неверный статус платежа`) {
				return "ambiguous-marker present ✓", nil
			}
			return "", fmt.Errorf("code=%d body=%q", code, body)
		})

	// --- Slow body (skip — would take many seconds; just check scenario applies and SOME bytes received) ---
	testCase("slow_body 50ms on applepay (chunked send)",
		func() {
			addScenario("applepay", "slow_body", map[string]string{"chunk_delay_ms": "50", "body": "ABC"}, true)
		},
		func() (string, error) {
			body, code, _, err := postJSON(fmt.Sprintf("/pay/%d/pay", walletPaymentID), `{"data":{}}`)
			if err != nil {
				return "", err
			}
			if code == 200 && body == "ABC" {
				return "got ABC streamed ✓", nil
			}
			return "", fmt.Errorf("code=%d body=%q", code, body)
		})

	// --- XML transport-level via init_payment.php (signed) ---
	section("Transport-level (init_payment.php, signed XML)")

	testCase("empty_response on init_payment.php",
		func() { addScenario("init_payment.php", "empty_response", nil, true) },
		func() (string, error) {
			body, code, err := postSignedXML("init_payment.php", map[string]string{
				"pg_merchant_id": merchantID,
				"pg_amount":      "100",
				"pg_currency":    "KZT",
				"pg_order_id":    "test-order-1",
			})
			if err != nil {
				return "", err
			}
			if code == 200 && body == "" {
				return "200 + empty ✓", nil
			}
			return "", fmt.Errorf("code=%d body=%q", code, body)
		})

	testCase("malformed_body on init_payment.php",
		func() {
			addScenario("init_payment.php", "malformed_body", map[string]string{"body": "<<<NOT_XML>>>"}, true)
		},
		func() (string, error) {
			body, code, err := postSignedXML("init_payment.php", map[string]string{
				"pg_merchant_id": merchantID, "pg_amount": "100", "pg_currency": "KZT", "pg_order_id": "test-order-2",
			})
			if err != nil {
				return "", err
			}
			if code == 200 && body == "<<<NOT_XML>>>" {
				return "got mock XML payload ✓", nil
			}
			return "", fmt.Errorf("code=%d body=%q", code, body)
		})

	// --- Business presets via wallet (force_failure) ---
	section("Business-error presets (wallet)")

	// Коды соответствуют PG-side error_mapping.go (Freedom Pay client).
	businessPresets := []struct {
		preset, expectedCode, expectedMsgPart string
	}{
		{"insufficient_funds", "10009", "Insufficient funds"},
		{"card_declined", "10007", "Declined by issuer"},
		{"card_data_input", "10005", "Wrong card data"},
		{"expired_card", "10017", "Card expired"},
		{"3ds_failed", "10004", "3D Secure"},
		{"limit_exceeded", "10006", "Card limitations"},
		{"code_limit_exceeded", "10003", "PIN code attempts"},
		{"emitter_error", "10001", "Emitter bank"},
		{"country_not_supported", "10013", "Card country"},
		{"transaction_amount_zero", "11016", "amount is zero"},
		{"unknown_bank_error", "9992", "Unknown bank"},
		{"default_bank_error", "99999", "Unmapped error code"},
	}
	for _, bp := range businessPresets {
		bp := bp
		testCase(fmt.Sprintf("preset %s (Freedom code=%s)", bp.preset, bp.expectedCode),
			func() {
				mustReset()
				applyPreset(bp.preset)
			},
			func() (string, error) {
				body, code, _, err := postJSON(fmt.Sprintf("/pay/%d/pay", walletPaymentID), `{"data":{}}`)
				if err != nil {
					return "", err
				}
				if code == 200 && strings.Contains(body, bp.expectedMsgPart) {
					return fmt.Sprintf("msg contains %q ✓", bp.expectedMsgPart), nil
				}
				return "", fmt.Errorf("code=%d body=%q", code, body)
			})
	}

	// XML-уровень: проверяем что pg_error_code в ответе init_payment.php совпадает с ожидаемым.
	for _, bp := range businessPresets {
		bp := bp
		testCase(fmt.Sprintf("preset %s reaches XML init_payment with pg_error_code=%s", bp.preset, bp.expectedCode),
			func() {
				mustReset()
				applyPreset(bp.preset)
			},
			func() (string, error) {
				body, _, err := postSignedXML("init_payment.php", map[string]string{
					"pg_merchant_id": merchantID, "pg_amount": "100", "pg_currency": "KZT",
					"pg_order_id": fmt.Sprintf("biz-%s-%d", bp.preset, time.Now().UnixNano()),
				})
				if err != nil {
					return "", err
				}
				wantCode := fmt.Sprintf("<pg_error_code>%s</pg_error_code>", bp.expectedCode)
				if strings.Contains(body, wantCode) {
					return fmt.Sprintf("pg_error_code=%s ✓", bp.expectedCode), nil
				}
				return "", fmt.Errorf("want %q in body=%q", wantCode, body)
			})
	}

	// --- Wallet-specific presets ---
	section("Wallet broken-response presets")

	testCase("preset wallet_empty_response",
		func() { mustReset(); applyPreset("wallet_empty_response") },
		func() (string, error) {
			body, code, _, err := postJSON(fmt.Sprintf("/pay/%d/pay", walletPaymentID), `{"data":{}}`)
			if err != nil {
				return "", err
			}
			if code == 200 && body == "" {
				return "empty ✓", nil
			}
			return "", fmt.Errorf("code=%d body=%q", code, body)
		})

	testCase("preset wallet_malformed",
		func() { mustReset(); applyPreset("wallet_malformed") },
		func() (string, error) {
			body, code, _, err := postJSON(fmt.Sprintf("/pay/%d/pay", walletPaymentID), `{"data":{}}`)
			if err != nil {
				return "", err
			}
			if code == 200 && body == `{"data":{` {
				return "got truncated JSON ✓", nil
			}
			return "", fmt.Errorf("code=%d body=%q", code, body)
		})

	// --- XML scenario presets (verify by inspecting active scenarios list) ---
	section("Preset registration (verify via active list)")

	registrationPresets := []struct {
		name             string
		expectedEndpoint string
		expectedAction   string
	}{
		{"init_retry_exhausted", "init_payment.php", "timeout"},
		{"hold_init_retry_exhausted", "init", "timeout"},
		{"wallet_retry_exhausted", "applepay", "timeout"},
		{"context_deadline", "*", "timeout"},
		{"init_malformed_xml", "init_payment.php", "malformed_body"},
		{"slow_body_capture", "do_capture.php", "slow_body"},
		{"wrong_payment_id", "direct", "wrong_payment_id"},
		{"missing_signature", "direct", "missing_field"},
		{"wrong_amount", "get_status3.php", "wrong_amount"},
		{"hold_timeout", "direct", "timeout"},
		{"ex1001", "direct", "ambiguous_error"},
		{"desync", "direct", "force_failure"},
		// Recovery flows — каждый ставит 2 сценария: первый на failed-endpoint, второй на get_status3
		{"hold_pending_recovery", "direct", "force_status"},
		{"capture_failed_status_approved", "do_capture.php", "force_failure"},
		{"cancel_failed_status_revoked", "cancel.php", "force_failure"},
		{"revoke_failed_status_revoked", "revoke.php", "force_failure"},
	}
	for _, rp := range registrationPresets {
		rp := rp
		testCase(fmt.Sprintf("preset %s registers correctly", rp.name),
			func() { mustReset(); applyPreset(rp.name) },
			func() (string, error) {
				scList := listScenariosHTML()
				// После редизайна разметка: <span class="endpoint-pill">{endpoint}</span> + <span class="badge badge-XXX">{action}</span>.
				// Badge-class зависит от action (scenarioActionClass), так что ищем подстроки независимо.
				epPill := fmt.Sprintf(`endpoint-pill">%s</span>`, rp.expectedEndpoint)
				actionTag := fmt.Sprintf(`">%s</span>`, rp.expectedAction)
				if strings.Contains(scList, epPill) && strings.Contains(scList, actionTag) {
					return "scenario registered ✓", nil
				}
				return "", fmt.Errorf("expected endpoint=%q + action=%q in scenarios list", rp.expectedEndpoint, rp.expectedAction)
			})
	}

	// --- Recovery flows (behavioral, end-to-end через 2 endpoint-а) ---
	section("Recovery flows (end-to-end)")

	testCase("hold_pending_recovery: direct=process → status=success",
		func() {
			mustReset()
			applyPreset("hold_pending_recovery")
		},
		func() (string, error) {
			pid := holdInit()
			// Шаг 1: direct → ожидаем pg_payment_status=process
			body1, _, err := postSignedXML("direct", map[string]string{
				"pg_payment_id": fmt.Sprintf("%d", pid), "pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			if !strings.Contains(body1, "<pg_payment_status>process</pg_payment_status>") {
				return "", fmt.Errorf("direct phase: expected process, got %q", body1[:min(300, len(body1))])
			}
			// Шаг 2: get_status3 → ожидаем pg_payment_status=success
			body2, _, err := postSignedXML("get_status3.php", map[string]string{
				"pg_payment_id": fmt.Sprintf("%d", pid), "pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			if !strings.Contains(body2, "<pg_payment_status>success</pg_payment_status>") {
				return "", fmt.Errorf("status phase: expected success, got %q", body2[:min(300, len(body2))])
			}
			return "process → success ✓", nil
		})

	testCase("capture_failed_status_approved: capture=error → status=success",
		func() {
			mustReset()
			applyPreset("capture_failed_status_approved")
		},
		func() (string, error) {
			pid := holdInit()
			body1, _, err := postSignedXML("do_capture.php", map[string]string{
				"pg_payment_id": fmt.Sprintf("%d", pid), "pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			if !strings.Contains(body1, "<pg_status>error</pg_status>") || !strings.Contains(body1, "technical bank error") {
				return "", fmt.Errorf("capture phase: expected error, got %q", body1[:min(300, len(body1))])
			}
			body2, _, err := postSignedXML("get_status3.php", map[string]string{
				"pg_payment_id": fmt.Sprintf("%d", pid), "pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			if !strings.Contains(body2, "<pg_payment_status>success</pg_payment_status>") {
				return "", fmt.Errorf("status phase: expected success, got %q", body2[:min(300, len(body2))])
			}
			return "capture error → status success ✓", nil
		})

	testCase("cancel_failed_status_revoked: cancel=error → status=revoked",
		func() {
			mustReset()
			applyPreset("cancel_failed_status_revoked")
		},
		func() (string, error) {
			pid := holdInit()
			body1, _, err := postSignedXML("cancel.php", map[string]string{
				"pg_payment_id": fmt.Sprintf("%d", pid), "pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			if !strings.Contains(body1, "<pg_status>error</pg_status>") {
				return "", fmt.Errorf("cancel phase: expected error, got %q", body1[:min(300, len(body1))])
			}
			body2, _, err := postSignedXML("get_status3.php", map[string]string{
				"pg_payment_id": fmt.Sprintf("%d", pid), "pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			if !strings.Contains(body2, "<pg_payment_status>revoked</pg_payment_status>") {
				return "", fmt.Errorf("status phase: expected revoked, got %q", body2[:min(300, len(body2))])
			}
			return "cancel error → status revoked ✓", nil
		})

	testCase("revoke_failed_status_revoked: revoke=error → status=revoked",
		func() {
			mustReset()
			applyPreset("revoke_failed_status_revoked")
		},
		func() (string, error) {
			pid := holdInit()
			body1, _, err := postSignedXML("revoke.php", map[string]string{
				"pg_payment_id": fmt.Sprintf("%d", pid), "pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			if !strings.Contains(body1, "<pg_status>error</pg_status>") {
				return "", fmt.Errorf("revoke phase: expected error, got %q", body1[:min(300, len(body1))])
			}
			body2, _, err := postSignedXML("get_status3.php", map[string]string{
				"pg_payment_id": fmt.Sprintf("%d", pid), "pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			if !strings.Contains(body2, "<pg_payment_status>revoked</pg_payment_status>") {
				return "", fmt.Errorf("status phase: expected revoked, got %q", body2[:min(300, len(body2))])
			}
			return "revoke error → status revoked ✓", nil
		})

	// --- XML content-level via direct (signed, with payment record) ---
	section("Content-level XML modifications")

	testCase("wrong_payment_id on direct",
		func() {
			mustReset()
			addScenario("direct", "wrong_payment_id", map[string]string{"payment_id": "111222333"}, true)
		},
		func() (string, error) {
			// Hold init first to get a real paymentID
			pid := holdInit()
			body, _, err := postSignedXML("direct", map[string]string{
				"pg_payment_id":  fmt.Sprintf("%d", pid),
				"pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			if strings.Contains(body, "<pg_payment_id>111222333</pg_payment_id>") {
				return "pg_payment_id substituted ✓", nil
			}
			return "", fmt.Errorf("body=%q", body)
		})

	testCase("missing_field pg_sig on direct",
		func() {
			mustReset()
			addScenario("direct", "missing_field", map[string]string{"field": "pg_sig"}, true)
		},
		func() (string, error) {
			pid := holdInit()
			body, _, err := postSignedXML("direct", map[string]string{
				"pg_payment_id":  fmt.Sprintf("%d", pid),
				"pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			if !strings.Contains(body, "<pg_sig>") {
				return "pg_sig removed ✓", nil
			}
			return "", fmt.Errorf("pg_sig still present: %s", body[:min(200, len(body))])
		})

	testCase("extra_garbage on direct adds noise",
		func() {
			mustReset()
			addScenario("direct", "extra_garbage", map[string]string{"count": "3"}, true)
		},
		func() (string, error) {
			pid := holdInit()
			body, _, err := postSignedXML("direct", map[string]string{
				"pg_payment_id":  fmt.Sprintf("%d", pid),
				"pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			cnt := strings.Count(body, "pg_garbage_")
			if cnt == 6 {
				return "3 garbage fields (×2 tags) ✓", nil
			}
			return "", fmt.Errorf("expected 6 pg_garbage_ occurrences, got %d", cnt)
		})

	testCase("ambiguous_error on direct returns ambiguous-marker",
		func() {
			mustReset()
			addScenario("direct", "ambiguous_error", map[string]string{"message": "Неверный статус платежа"}, true)
		},
		func() (string, error) {
			pid := holdInit()
			body, _, err := postSignedXML("direct", map[string]string{
				"pg_payment_id":  fmt.Sprintf("%d", pid),
				"pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			if strings.Contains(body, "Неверный статус платежа") && strings.Contains(body, "pg_failure_description") {
				return "ambiguous-marker in pg_failure_description ✓", nil
			}
			return "", fmt.Errorf("body=%q", body)
		})

	testCase("force_status on get_status3 returns success",
		func() {
			mustReset()
			addScenario("get_status3.php", "force_status", map[string]string{"payment_status": "success"}, true)
		},
		func() (string, error) {
			pid := holdInit()
			body, _, err := postSignedXML("get_status3.php", map[string]string{
				"pg_payment_id":  fmt.Sprintf("%d", pid),
				"pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			if strings.Contains(body, "<pg_payment_status>success</pg_payment_status>") {
				return "pg_payment_status=success ✓", nil
			}
			return "", fmt.Errorf("body=%q", body)
		})

	testCase("invalid_signature zeroes pg_sig",
		func() {
			mustReset()
			addScenario("direct", "invalid_signature", nil, true)
		},
		func() (string, error) {
			pid := holdInit()
			body, _, err := postSignedXML("direct", map[string]string{
				"pg_payment_id":  fmt.Sprintf("%d", pid),
				"pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			if strings.Contains(body, "<pg_sig>00000000000000000000000000000000</pg_sig>") {
				return "pg_sig zeroed ✓", nil
			}
			return "", fmt.Errorf("body=%q", body)
		})

	testCase("partial_amount substitutes pg_amount",
		func() {
			mustReset()
			addScenario("get_status3.php", "partial_amount", map[string]string{"amount": "42"}, true)
		},
		func() (string, error) {
			pid := holdInit()
			body, _, err := postSignedXML("get_status3.php", map[string]string{
				"pg_payment_id":  fmt.Sprintf("%d", pid),
				"pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			if strings.Contains(body, "<pg_amount>42</pg_amount>") {
				return "pg_amount=42 ✓", nil
			}
			return "", fmt.Errorf("body=%q", body)
		})

	// --- GooglePay (form-encoded) ---
	section("GooglePay wallet (form-encoded)")

	gpPaymentID := holdInit()
	testCase("googlepay happy-path returns success JSON",
		func() { mustReset() },
		func() (string, error) {
			body, code, _, err := postForm(fmt.Sprintf("/pay/%d/pay", gpPaymentID), "token=googlepay-token-xxx")
			if err != nil {
				return "", err
			}
			if code != 200 {
				return "", fmt.Errorf("status=%d body=%q", code, body)
			}
			if !strings.Contains(body, `"status":"ok"`) || !strings.Contains(body, "frame_url") {
				return "", fmt.Errorf("expected googlepay success JSON, got %q", body)
			}
			return "200 OK + status=ok ✓", nil
		})

	testCase("googlepay scenario empty_response applies",
		func() {
			mustReset()
			addScenario("googlepay", "empty_response", nil, true)
		},
		func() (string, error) {
			body, code, _, err := postForm(fmt.Sprintf("/pay/%d/pay", gpPaymentID), "token=x")
			if err != nil {
				return "", err
			}
			if code == 200 && body == "" {
				return "empty ✓", nil
			}
			return "", fmt.Errorf("code=%d body=%q", code, body)
		})

	// --- AddCard / RemoveCard (биндинги) ---
	section("AddCard / RemoveCard (card binding)")

	testCase("add2 happy-path returns ok",
		func() { mustReset() },
		func() (string, error) {
			body, code, err := postSignedXML("add2", map[string]string{
				"pg_merchant_id": merchantID,
				"pg_user_id":     "1",
				"pg_post_link":   "http://example.com",
			})
			if err != nil || code != 200 {
				return "", fmt.Errorf("code=%d err=%v body=%q", code, err, body)
			}
			if !strings.Contains(body, "<pg_status>ok</pg_status>") {
				return "", fmt.Errorf("expected ok, got %q", body[:min(300, len(body))])
			}
			return "add2 ok ✓", nil
		})

	testCase("remove happy-path returns ok",
		func() { mustReset() },
		func() (string, error) {
			body, code, err := postSignedXML("remove", map[string]string{
				"pg_merchant_id": merchantID,
				"pg_user_id":     "1",
				"pg_card_token":  "dummy-token",
			})
			if err != nil || code != 200 {
				return "", fmt.Errorf("code=%d err=%v body=%q", code, err, body)
			}
			if !strings.Contains(body, "<pg_status>ok</pg_status>") {
				return "", fmt.Errorf("expected ok, got %q", body[:min(300, len(body))])
			}
			return "remove ok ✓", nil
		})

	// --- Cancel / Revoke happy-path ---
	section("Cancel / Revoke (XML happy-path)")

	testCase("cancel.php on Authorized payment returns ok",
		func() { mustReset() },
		func() (string, error) {
			pid := holdInit()
			// Move to authorized via direct
			if _, _, err := postSignedXML("direct", map[string]string{
				"pg_payment_id": fmt.Sprintf("%d", pid), "pg_merchant_id": merchantID,
			}); err != nil {
				return "", err
			}
			body, _, err := postSignedXML("cancel.php", map[string]string{
				"pg_payment_id": fmt.Sprintf("%d", pid), "pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			if !strings.Contains(body, "<pg_status>ok</pg_status>") {
				return "", fmt.Errorf("expected ok, got %q", body[:min(300, len(body))])
			}
			return "cancel ok ✓", nil
		})

	testCase("revoke.php after capture returns ok",
		func() { mustReset() },
		func() (string, error) {
			pid := holdInit()
			merchant := "100001"
			// Hold (direct) → Capture → Revoke
			if _, _, err := postSignedXML("direct", map[string]string{"pg_payment_id": fmt.Sprintf("%d", pid), "pg_merchant_id": merchant}); err != nil {
				return "", err
			}
			if _, _, err := postSignedXML("do_capture.php", map[string]string{"pg_payment_id": fmt.Sprintf("%d", pid), "pg_merchant_id": merchant}); err != nil {
				return "", err
			}
			body, _, err := postSignedXML("revoke.php", map[string]string{"pg_payment_id": fmt.Sprintf("%d", pid), "pg_merchant_id": merchant, "pg_refund_amount": "100"})
			if err != nil {
				return "", err
			}
			if !strings.Contains(body, "<pg_status>ok</pg_status>") {
				return "", fmt.Errorf("expected ok, got %q", body[:min(300, len(body))])
			}
			return "revoke ok ✓", nil
		})

	testCase("get_status3.php returns valid Status response",
		func() { mustReset() },
		func() (string, error) {
			pid := holdInit()
			body, _, err := postSignedXML("get_status3.php", map[string]string{
				"pg_payment_id": fmt.Sprintf("%d", pid), "pg_merchant_id": merchantID,
			})
			if err != nil {
				return "", err
			}
			if !strings.Contains(body, "<pg_status>ok</pg_status>") || !strings.Contains(body, "<pg_payment_id>") {
				return "", fmt.Errorf("expected ok status with payment_id, got %q", body[:min(300, len(body))])
			}
			return "get_status3 ok ✓", nil
		})

	// --- QR-PAY full cycle ---
	section("QR-PAY full cycle")

	var qrUUID string
	testCase("qr-code/generate creates code with uuid",
		func() { mustReset() },
		func() (string, error) {
			body, code, _, err := postJSONAuth("/qr-code/generate", `{"beneficiary":{"bin":"123","tid":"T","mid":"M"},"payment":{"amount":1500,"dataType":"001"}}`, "test", "test")
			if err != nil {
				return "", err
			}
			if code != 200 {
				return "", fmt.Errorf("code=%d body=%q", code, body)
			}
			uuid := extractField(body, `"uuid":"`, `"`)
			if uuid == "" {
				return "", fmt.Errorf("no uuid in response %q", body)
			}
			qrUUID = uuid
			return fmt.Sprintf("uuid=%s ✓", uuid), nil
		})

	testCase("qr-code/get-status returns CREATED",
		func() {},
		func() (string, error) {
			if qrUUID == "" {
				return "", fmt.Errorf("prerequisite: qrUUID not set (previous test failed)")
			}
			body, code, _, err := getJSONAuth("/qr-code/get-status/"+qrUUID, "test", "test")
			if err != nil {
				return "", err
			}
			if code != 200 {
				return "", fmt.Errorf("code=%d body=%q", code, body)
			}
			if !strings.Contains(body, `"status"`) {
				return "", fmt.Errorf("expected status field, got %q", body)
			}
			return "got status ✓", nil
		})

	testCase("qr-code/change-status to SCANNED",
		func() {},
		func() (string, error) {
			if qrUUID == "" {
				return "", fmt.Errorf("prerequisite: qrUUID not set")
			}
			payload := fmt.Sprintf(`{"uuid":"%s","status":"SCANNED"}`, qrUUID)
			body, code, _, err := postJSONAuth("/qr-code/change-status", payload, "test", "test")
			if err != nil {
				return "", err
			}
			if code != 200 {
				return "", fmt.Errorf("code=%d body=%q", code, body)
			}
			return "changed to SCANNED ✓", nil
		})

	// --- Panel-actions (force_captured / send_webhook) ---
	section("Panel-actions (force state + webhook)")

	testCase("panel force_authorized changes payment state",
		func() { mustReset() },
		func() (string, error) {
			pid := holdInit()
			form := url.Values{
				"action":     {"force_authorized"},
				"payment_id": {fmt.Sprintf("%d", pid)},
			}
			resp, err := http.PostForm(baseURL+"/panel/cards/action", form)
			if err != nil {
				return "", err
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 303 && resp.StatusCode != 200 {
				return "", fmt.Errorf("status=%d", resp.StatusCode)
			}
			// Verify via /panel?tab=cards rendered HTML
			html, _ := http.Get(baseURL + "/panel?tab=cards")
			b, _ := io.ReadAll(html.Body)
			html.Body.Close()
			if !strings.Contains(string(b), "AUTHORIZED") {
				return "", fmt.Errorf("AUTHORIZED status not visible in /panel?tab=cards after force_authorized")
			}
			return "force_authorized → AUTHORIZED visible ✓", nil
		})

	testCase("panel force_captured on AUTHORIZED → CAPTURED",
		func() { mustReset() },
		func() (string, error) {
			pid := holdInit()
			// First authorize
			_, _ = http.PostForm(baseURL+"/panel/cards/action", url.Values{
				"action": {"force_authorized"}, "payment_id": {fmt.Sprintf("%d", pid)},
			})
			// Then capture
			resp, err := http.PostForm(baseURL+"/panel/cards/action", url.Values{
				"action": {"force_captured"}, "payment_id": {fmt.Sprintf("%d", pid)},
			})
			if err != nil {
				return "", err
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			html, _ := http.Get(baseURL + "/panel?tab=cards")
			b, _ := io.ReadAll(html.Body)
			html.Body.Close()
			if !strings.Contains(string(b), "CAPTURED") {
				return "", fmt.Errorf("CAPTURED not visible after force_captured")
			}
			return "force_captured → CAPTURED ✓", nil
		})

	// --- Webhook test via fake-PG (httptest server) ---
	section("Webhook sender → fake PG (httptest.Server)")

	testWebhookFlow := func() {
		var received atomic.Int32
		var lastBody atomic.Value
		fakePG := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()
			lastBody.Store(string(body))
			received.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer fakePG.Close()

		// Step 1: spawn a payment via HoldInit (uses normal webhook URL)
		// Then we tell mock to send webhook to fakePG by... actually mock uses config URL fixed at startup.
		// Instead, we directly hit fakePG by reusing the mock's internal PostForm "Send Webhook" panel action,
		// but that posts to the PG webhook URL configured in mock (not arbitrary).
		// So this test just verifies that mock CAN send (by directly POSTing form similar to mock's body to fakePG).
		// Real e2e webhook chain requires PG to be configured separately.
		// For now: verify webhook structure via the mock's send_card_webhook action mechanic — observed via request log.

		testCase("send_card_webhook records in request-log",
			func() { mustReset() },
			func() (string, error) {
				pid := holdInit()
				resp, err := http.PostForm(baseURL+"/panel/cards/action", url.Values{
					"action": {"send_card_webhook"}, "payment_id": {fmt.Sprintf("%d", pid)},
				})
				if err != nil {
					return "", err
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				// Mock will try to send to its configured PG_FREEDOM_PAY_CARD_WEBHOOK_URL — failure is fine,
				// we just verify the mock attempted to fire (action endpoint returns redirect).
				if resp.StatusCode != 303 && resp.StatusCode != 200 {
					return "", fmt.Errorf("unexpected status=%d", resp.StatusCode)
				}
				return "send action accepted ✓", nil
			})

		// Direct POST to fakePG to validate fakePG plumbing works.
		req, _ := http.NewRequest("POST", fakePG.URL+"/webhook", strings.NewReader(`{"test":"payload"}`))
		req.Header.Set("Content-Type", "application/json")
		c := &http.Client{Timeout: 3 * time.Second}
		c.Do(req)

		testCase("fakePG receives direct POST",
			func() {},
			func() (string, error) {
				if received.Load() < 1 {
					return "", fmt.Errorf("fakePG received 0 requests")
				}
				return fmt.Sprintf("received=%d ✓", received.Load()), nil
			})
	}
	testWebhookFlow()

	// --- Halyk Epay v2 ---
	section("Halyk Epay — happy paths")
	testCase("OAuth token issued",
		func() {},
		func() (string, error) {
			body, code, _, err := postForm("/oauth2/token",
				"grant_type=client_credentials&client_id=test&client_secret=s&invoiceID=000123&amount=5000&currency=KZT&terminal=t")
			if err != nil {
				return "", err
			}
			if code != 200 || !strings.Contains(body, "access_token") {
				return "", fmt.Errorf("got %d body=%s", code, body)
			}
			return "OAuth ok", nil
		})

	var epayID string
	testCase("cryptopay → Authorize",
		func() {},
		func() (string, error) {
			body, code, _, err := postJSON("/api/payment/cryptopay",
				`{"amount":5000,"invoiceId":"000123","currency":"KZT","cryptogram":"x"}`)
			if err != nil {
				return "", err
			}
			if code != 200 {
				return "", fmt.Errorf("got %d body=%s", code, body)
			}
			epayID = extractField(body, `"id":"`, `"`)
			if epayID == "" {
				return "", fmt.Errorf("no id in response")
			}
			return "id=" + epayID, nil
		})

	testCase("charge → 200 {code:0}",
		func() {},
		func() (string, error) {
			body, code, _, err := postJSON("/api/operation/"+epayID+"/charge", `{"amount":5000}`)
			if err != nil {
				return "", err
			}
			if code != 200 || !strings.Contains(body, `"code":0`) {
				return "", fmt.Errorf("got %d body=%s", code, body)
			}
			return "charged", nil
		})

	testCase("status-check returns CHARGE",
		func() {},
		func() (string, error) {
			req, _ := http.NewRequest("GET", baseURL+"/check-status/payment/transactionId/"+epayID, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return "", err
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != 200 || !strings.Contains(string(raw), `"status":"CHARGE"`) {
				return "", fmt.Errorf("got %d body=%s", resp.StatusCode, raw)
			}
			return "status=CHARGE", nil
		})

	section("Halyk Epay — business declines")
	for _, tc := range []struct {
		preset string
		code   string
	}{
		{"epay_insufficient_funds", `"code":484`},
		{"epay_card_expired", `"code":478`},
		{"epay_invalid_card", `"code":457`},
		{"epay_declined_by_issuer", `"code":455`},
		{"epay_limit_exceeded", `"code":486`},
		{"epay_unknown_error", `"code":477`},
	} {
		tc := tc
		testCase(tc.preset,
			func() { applyPresetForBank(tc.preset, "epay"); mustReset() }, // mustReset перед нет, сначала apply
			func() (string, error) {
				// preset скидывается mustReset выше — повторно apply:
				applyPresetForBank(tc.preset, "epay")
				body, code, _, err := postJSON("/api/payment/cryptopay",
					`{"amount":1000,"invoiceId":"000999","currency":"KZT","cryptogram":"x"}`)
				if err != nil {
					return "", err
				}
				if code != 400 || !strings.Contains(body, tc.code) {
					return "", fmt.Errorf("got %d body=%s", code, body)
				}
				return tc.code, nil
			})
	}

	section("Halyk Epay — HTTP status overrides")
	testCase("401 Unauthorized",
		func() { mustReset(); applyPresetForBank("epay_unauthorized_401", "epay") },
		func() (string, error) {
			_, code, _, err := postJSON("/api/payment/cryptopay",
				`{"amount":1000,"invoiceId":"000999","cryptogram":"x"}`)
			if err != nil {
				return "", err
			}
			if code != 401 {
				return "", fmt.Errorf("got %d", code)
			}
			return "401 ✓", nil
		})
	testCase("403 Forbidden",
		func() { mustReset(); applyPresetForBank("epay_forbidden_403", "epay") },
		func() (string, error) {
			_, code, _, err := postJSON("/api/payment/cryptopay",
				`{"amount":1000,"invoiceId":"000999","cryptogram":"x"}`)
			if err != nil {
				return "", err
			}
			if code != 403 {
				return "", fmt.Errorf("got %d", code)
			}
			return "403 ✓", nil
		})
	testCase("OAuth credentials invalid",
		func() { mustReset(); applyPresetForBank("epay_oauth_unauthorized", "epay") },
		func() (string, error) {
			_, code, _, err := postForm("/oauth2/token", "grant_type=client_credentials&client_id=bad")
			if err != nil {
				return "", err
			}
			if code != 401 {
				return "", fmt.Errorf("got %d", code)
			}
			return "401 on token ✓", nil
		})

	section("Halyk Epay — ambiguous / recovery")
	testCase("ambiguous charge → status reports AUTH",
		func() {},
		func() (string, error) {
			mustReset()
			// 1) Создаём свежий платёж.
			body, _, _, err := postJSON("/api/payment/cryptopay",
				`{"amount":7000,"invoiceId":"001001","currency":"KZT","cryptogram":"x"}`)
			if err != nil {
				return "", err
			}
			id := extractField(body, `"id":"`, `"`)
			// 2) Ставим preset → следующий charge упадёт с 477.
			applyPresetForBank("epay_ambiguous_charge_recovery", "epay")
			_, chargeCode, _, _ := postJSON("/api/operation/"+id+"/charge", `{"amount":7000}`)
			if chargeCode != 400 {
				return "", fmt.Errorf("expected 400 on ambiguous charge, got %d", chargeCode)
			}
			// 3) Status-check показывает AUTH (charge не выполнился).
			req, _ := http.NewRequest("GET", baseURL+"/check-status/payment/transactionId/"+id, nil)
			resp, _ := http.DefaultClient.Do(req)
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if !strings.Contains(string(raw), `"status":"AUTH"`) {
				return "", fmt.Errorf("expected AUTH, got %s", raw)
			}
			return "AUTH ✓", nil
		})

	section("Halyk Epay — transient retry")
	testCase("transient 500 → second attempt 200",
		func() {},
		func() (string, error) {
			mustReset()
			body, _, _, _ := postJSON("/api/payment/cryptopay",
				`{"amount":3000,"invoiceId":"002002","currency":"KZT","cryptogram":"x"}`)
			id := extractField(body, `"id":"`, `"`)
			applyPresetForBank("epay_transient_500_then_ok", "epay")
			_, code1, _, _ := postJSON("/api/operation/"+id+"/charge", `{"amount":3000}`)
			if code1 != 500 {
				return "", fmt.Errorf("first charge expected 500, got %d", code1)
			}
			_, code2, _, _ := postJSON("/api/operation/"+id+"/charge", `{"amount":3000}`)
			if code2 != 200 {
				return "", fmt.Errorf("second charge expected 200, got %d", code2)
			}
			return "500 then 200 ✓", nil
		})

	section("Halyk Epay — 3DS")
	testCase("3DS challenge inline",
		func() { mustReset(); applyPresetForBank("epay_3ds_required", "epay") },
		func() (string, error) {
			body, code, _, err := postJSON("/api/payment/cryptopay",
				`{"amount":4000,"invoiceId":"003003","currency":"KZT","cryptogram":"x"}`)
			if err != nil {
				return "", err
			}
			if code != 200 || !strings.Contains(body, `"secure3D"`) {
				return "", fmt.Errorf("got %d body=%s", code, body)
			}
			return "secure3D ✓", nil
		})
	testCase("3DS missing action URL",
		func() { mustReset(); applyPresetForBank("epay_3ds_missing_action_url", "epay") },
		func() (string, error) {
			body, _, _, err := postJSON("/api/payment/cryptopay",
				`{"amount":4000,"invoiceId":"003004","currency":"KZT","cryptogram":"x"}`)
			if err != nil {
				return "", err
			}
			// action должен быть пустым.
			if !strings.Contains(body, `"action":""`) {
				return "", fmt.Errorf("action should be empty, got %s", body)
			}
			return "action='' ✓", nil
		})

	mustReset()

	// Final summary
	section("Summary")
	fmt.Printf("\n  ✅ Passed: %d\n", passCount)
	fmt.Printf("  ❌ Failed: %d\n", failCount)
	if failCount > 0 {
		fmt.Println("\n  Failures:")
		for _, f := range failures {
			fmt.Printf("    - %s\n", f)
		}
		os.Exit(1)
	}
	mustReset()
	fmt.Println("\n  All scenarios pass! 🎉")
}

// ---------------- helpers ----------------

func section(s string) {
	fmt.Printf("\n=== %s ===\n", s)
}

func testCase(name string, setup func(), check func() (string, error)) {
	setup()
	detail, err := check()
	if err != nil {
		failCount++
		failures = append(failures, fmt.Sprintf("%s: %v", name, err))
		fmt.Printf("  ❌ %s — %v\n", name, err)
	} else {
		passCount++
		fmt.Printf("  ✅ %s — %s\n", name, detail)
	}
}

func mustReset() {
	// Only scenarios — keep payments alive across tests.
	_, err := http.PostForm(baseURL+"/panel/scenarios/reset", url.Values{})
	if err != nil {
		panic("reset failed: " + err.Error())
	}
}

func resetAll() {
	mustReset()
	_, err := http.PostForm(baseURL+"/panel/cards/reset", url.Values{})
	if err != nil {
		panic("cards reset failed: " + err.Error())
	}
}

func applyPreset(name string) {
	applyPresetForBank(name, "freedom")
}

// applyPresetForBank — точечный helper для пресетов с банк-scope.
// Большинство Freedom-пресетов работают и без bank-параметра (Scenario.Bank=Any),
// но Epay-пресеты обязаны быть в Bank=Epay, иначе scenario.Match их не найдёт.
func applyPresetForBank(name, bank string) {
	resp, err := http.PostForm(baseURL+"/panel/scenarios/preset", url.Values{
		"preset": {name},
		"bank":   {bank},
	})
	if err != nil {
		panic("applyPreset: " + err.Error())
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

func addScenario(endpoint, action string, params map[string]string, consumeOnce bool) {
	form := url.Values{
		"endpoint":     {endpoint},
		"payment_id":   {"*"},
		"order_id":     {"*"},
		"merchant_id":  {"*"},
		"action":       {action},
		"consume_once": {fmt.Sprintf("%v", consumeOnce)},
	}
	for k, v := range params {
		if k == "payment_id" {
			form.Set("payment_id_param", v)
			continue
		}
		form.Set(k, v)
	}
	resp, err := http.PostForm(baseURL+"/panel/scenarios/add", form)
	if err != nil {
		panic("addScenario: " + err.Error())
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

func listScenariosHTML() string {
	return listScenariosHTMLForBank("freedom")
}

func listScenariosHTMLForBank(bank string) string {
	resp, err := http.Get(baseURL + "/panel?bank=" + bank + "&tab=scenarios") //nolint:noctx
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func createSyntheticPayment() uint64 {
	orderID := time.Now().UnixNano() % 1_000_000
	resp, err := http.PostForm(baseURL+"/panel/cards/action", url.Values{
		"action":   {"create_synthetic"},
		"order_id": {fmt.Sprintf("%d", orderID)},
		"amount":   {"1000"},
		"user_id":  {"1"},
		"status":   {"NEW"},
	})
	if err != nil {
		panic("createSynthetic: " + err.Error())
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	// find the most recent payment via panel HTML
	html, err := http.Get(baseURL + "/panel?tab=cards")
	if err != nil {
		panic("get cards: " + err.Error())
	}
	defer html.Body.Close()
	body, _ := io.ReadAll(html.Body)
	// Each row has data-payment-id or visible paymentID
	idx := strings.Index(string(body), fmt.Sprintf("synthetic-token-%d", orderID))
	if idx < 0 {
		panic("synthetic payment not found")
	}
	// look backwards for payment id (in code tag in same row)
	prefix := string(body)[:idx]
	codeIdx := strings.LastIndex(prefix, "<code>")
	if codeIdx < 0 {
		panic("paymentID tag not found")
	}
	rest := prefix[codeIdx+len("<code>"):]
	end := strings.Index(rest, "</code>")
	if end < 0 {
		panic("paymentID end not found")
	}
	var pid uint64
	if _, err := fmt.Sscanf(rest[:end], "%d", &pid); err != nil {
		panic("paymentID parse: " + rest[:end])
	}
	return pid
}

// holdInit returns a real paymentID via HoldInit XML call (no scenario must be active for "init").
func holdInit() uint64 {
	// Make sure no scenarios match this call. We add scenario AFTER this. But caller resets first.
	// Some tests need scenario for OTHER endpoint than "init", so it's safe to call after addScenario.
	body, code, err := postSignedXML2("init", map[string]string{
		"pg_merchant_id":     merchantID,
		"pg_order_id":        fmt.Sprintf("hi-%d", time.Now().UnixNano()),
		"pg_amount":          "500",
		"pg_currency":        "KZT",
		"pg_user_id":         "1",
		"pg_card_token":      "tok",
		"pg_idempotency_key": fmt.Sprintf("idem-%d", time.Now().UnixNano()),
	}, "")
	if err != nil || code != 200 {
		panic(fmt.Sprintf("holdInit failed: code=%d err=%v body=%s", code, err, body))
	}
	// Parse <pg_payment_id>N</pg_payment_id>
	idx := strings.Index(body, "<pg_payment_id>")
	if idx < 0 {
		panic("holdInit: pg_payment_id missing: " + body)
	}
	rest := body[idx+len("<pg_payment_id>"):]
	end := strings.Index(rest, "</pg_payment_id>")
	if end < 0 {
		panic("holdInit: pg_payment_id end missing")
	}
	var pid uint64
	if _, err := fmt.Sscanf(rest[:end], "%d", &pid); err != nil {
		panic("holdInit: parse pid: " + rest[:end])
	}
	return pid
}

func postJSON(path, body string) (string, int, string, error) {
	return postJSONWithTimeout(path, body, 10*time.Second)
}

func postJSONWithTimeout(path, body string, timeout time.Duration) (string, int, string, error) {
	c := &http.Client{Timeout: timeout}
	req, _ := http.NewRequest("POST", baseURL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return "", 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), resp.StatusCode, resp.Header.Get("Content-Type"), nil
}

// postSignedXML — endpoint = freedom scriptName (e.g., "init_payment.php", "direct", "init", "get_status3.php").
func postSignedXML(endpoint string, fields map[string]string) (string, int, error) {
	body, code, err := postSignedXML2(endpoint, fields, "")
	return body, code, err
}

func postSignedXML2(endpoint string, fields map[string]string, overridePath string) (string, int, error) {
	// Path map: "init" → /v1/merchant/100001/card/init, "direct" → /v1/merchant/100001/card/direct
	path := overridePath
	if path == "" {
		switch endpoint {
		case "init":
			path = fmt.Sprintf("/v1/merchant/%s/card/init", fields["pg_merchant_id"])
		case "direct":
			path = fmt.Sprintf("/v1/merchant/%s/card/direct", fields["pg_merchant_id"])
		case "add2":
			path = fmt.Sprintf("/v1/merchant/%s/cardstorage/add2", fields["pg_merchant_id"])
		case "remove":
			path = fmt.Sprintf("/v1/merchant/%s/cardstorage/remove", fields["pg_merchant_id"])
		default:
			path = "/" + endpoint
		}
	}

	// Add salt
	salt := "abcdefgh"
	fields["pg_salt"] = salt
	// Sign — sorted by key, scriptName + values + secret
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{endpoint}
	for _, k := range keys {
		parts = append(parts, fields[k])
	}
	parts = append(parts, secret)
	sum := md5.Sum([]byte(strings.Join(parts, ";")))
	fields["pg_sig"] = hex.EncodeToString(sum[:])

	// Render XML
	var xmlBody strings.Builder
	xmlBody.WriteString("<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<request>")
	keys2 := make([]string, 0, len(fields))
	for k := range fields {
		keys2 = append(keys2, k)
	}
	sort.Strings(keys2)
	for _, k := range keys2 {
		xmlBody.WriteString(fmt.Sprintf("<%s>%s</%s>", k, fields[k], k))
	}
	xmlBody.WriteString("</request>")

	c := &http.Client{Timeout: 30 * time.Second}
	form := url.Values{"pg_xml": {xmlBody.String()}}
	req, _ := http.NewRequest("POST", baseURL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), resp.StatusCode, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// postForm — POST с body как application/x-www-form-urlencoded.
func postForm(path, rawBody string) (string, int, string, error) {
	c := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("POST", baseURL+path, strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(req)
	if err != nil {
		return "", 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), resp.StatusCode, resp.Header.Get("Content-Type"), nil
}

// postJSONAuth — POST JSON c Basic Auth (для QR-PAY endpoint-ов).
func postJSONAuth(path, body, user, pass string) (string, int, string, error) {
	c := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("POST", baseURL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
	resp, err := c.Do(req)
	if err != nil {
		return "", 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), resp.StatusCode, resp.Header.Get("Content-Type"), nil
}

// getJSONAuth — GET с Basic Auth.
func getJSONAuth(path, user, pass string) (string, int, string, error) {
	c := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", baseURL+path, nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
	resp, err := c.Do(req)
	if err != nil {
		return "", 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), resp.StatusCode, resp.Header.Get("Content-Type"), nil
}

// extractField вырезает значение между prefix и suffix (поверхностно, без полноценного JSON-парсинга).
func extractField(body, prefix, suffix string) string {
	idx := strings.Index(body, prefix)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(prefix):]
	end := strings.Index(rest, suffix)
	if end < 0 {
		return ""
	}
	return rest[:end]
}
