package pgclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/vevovip/chaospay/internal/domain/qr"
)

// QRClient отправляет JSON-webhook на /api/v1/payment-gateway/webhook/freedom-qr.
type QRClient struct {
	url    string
	client *http.Client
}

// NewQRClient конструктор.
func NewQRClient(webhookURL string) *QRClient {
	return &QRClient{
		url:    webhookURL,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

type qrPayload struct {
	UUID    string `json:"uuid"`
	Status  string `json:"status"`
	TrnID   int64  `json:"trnId,omitempty"`
	TrnDate string `json:"trnDate,omitempty"`
}

// Send отправляет QR-webhook. Если статус SUCCESS, добавляет trnId/trnDate.
func (c *QRClient) Send(code *qr.Code) (int, error) {
	if c.url == "" {
		return 0, fmt.Errorf("webhook url not set")
	}
	payload := qrPayload{UUID: code.UUID, Status: string(code.Status)}
	if code.Status == qr.StatusSuccess {
		payload.TrnID = code.TrnID
		payload.TrnDate = code.TrnDate
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal: %w", err)
	}
	resp, errDo := c.client.Post(c.url, "application/json", bytes.NewReader(body))
	if errDo != nil {
		log.Printf("[WEBHOOK qr] failed for %s: %v", code.UUID, errDo)
		return 0, errDo
	}
	defer resp.Body.Close()
	log.Printf("[WEBHOOK qr] sent %s status=%s → HTTP %d", code.UUID, code.Status, resp.StatusCode)
	return resp.StatusCode, nil
}
