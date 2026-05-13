// Package qr — оркестрация Single QR.
package qr

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/vevovip/chaospay/internal/domain/qr"
)

// Repository — контракт хранилища QR.
type Repository interface {
	Create(code *qr.Code)
	Get(uuid string) (*qr.Code, error)
	UpdateStatus(uuid string, status qr.Status) (*qr.Code, error)
	MarkWebhookSent(uuid string) (*qr.Code, error)
	List() []*qr.Code
	Reset()
	ListSuccessfulPayments(bin, tid, mid string) []*qr.Code
	ApplyRefundConfirmation(uuid, reference, parentTrnID string, amount float64) (*qr.Code, error)
}

// Generator генерирует PNG картинку QR в base64.
type Generator interface {
	PaymentURL(uuid string) string
	Generate(content string) (string, error)
}

// UUIDFactory — генератор уникальных идентификаторов.
type UUIDFactory func() string

// Webhook отправляет webhook в PG.
type Webhook interface {
	Send(code *qr.Code) (int, error)
}

// Service — application-сервис QR.
type Service struct {
	repo    Repository
	gen     Generator
	uuidFn  UUIDFactory
	webhook Webhook
}

// NewService конструктор.
func NewService(repo Repository, gen Generator, uuidFn UUIDFactory, wh Webhook) *Service {
	return &Service{repo: repo, gen: gen, uuidFn: uuidFn, webhook: wh}
}

// Repo возвращает репозиторий (для panel и refund-handler-а).
func (s *Service) Repo() Repository { return s.repo }

// Generate создаёт новый QR. dataType="003" → refund.
func (s *Service) Generate(in GenerateInput) (*qr.Code, error) {
	if in.BIN == "" {
		return nil, errors.New("bin is missing")
	}
	if in.TID == "" {
		return nil, errors.New("tid is missing")
	}
	if in.MID == "" {
		return nil, errors.New("mid is missing")
	}
	uuid := s.uuidFn()
	url := s.gen.PaymentURL(uuid)
	img, err := s.gen.Generate(url)
	if err != nil {
		return nil, fmt.Errorf("generate qr image: %w", err)
	}

	code := &qr.Code{
		UUID:       uuid,
		Status:     qr.StatusNew,
		Amount:     in.Amount,
		BIN:        in.BIN,
		TID:        in.TID,
		MID:        in.MID,
		QRBase64:   img,
		PaymentURL: url,
		IsRefund:   in.DataType == "003",
		CreatedAt:  time.Now(),
	}
	s.repo.Create(code)
	go s.autoExpire(uuid)
	return code, nil
}

// GetStatus — статус QR.
func (s *Service) GetStatus(uuid string) (*qr.Code, error) {
	return s.repo.Get(uuid)
}

// ChangeStatus — смена статуса (для отмены и т.п.).
func (s *Service) ChangeStatus(uuid string, status qr.Status) (*qr.Code, error) {
	updated, err := s.repo.UpdateStatus(uuid, status)
	if err != nil {
		return nil, err
	}
	if updated.Status.IsTerminal() {
		go s.maybeWebhook(updated)
	}
	return updated, nil
}

// SendWebhook вручную (panel button).
func (s *Service) SendWebhook(uuid string) (*qr.Code, error) {
	code, err := s.repo.MarkWebhookSent(uuid)
	if err != nil {
		return nil, err
	}
	go s.maybeWebhook(code)
	return code, nil
}

// ListRefundTransactions возвращает успешные оплаты у мерчанта (для refund SCANNED).
func (s *Service) ListRefundTransactions(bin, tid, mid string) []qr.RefundTransaction {
	payments := s.repo.ListSuccessfulPayments(bin, tid, mid)
	out := make([]qr.RefundTransaction, 0, len(payments))
	for _, p := range payments {
		out = append(out, qr.RefundTransaction{
			Reference:     fmt.Sprintf("ref-%s", shortID(p.UUID)),
			OperationDate: p.TrnDate,
			Amount:        p.Amount,
			TrnID:         p.TrnID,
		})
	}
	return out
}

// ConfirmRefund подтверждает возврат.
func (s *Service) ConfirmRefund(uuid, reference, parentTrnID string, amount float64) (*qr.Code, error) {
	return s.repo.ApplyRefundConfirmation(uuid, reference, parentTrnID, amount)
}

// autoExpire через qr.TTL переводит в EXPIRED.
func (s *Service) autoExpire(uuid string) {
	ctx, cancel := context.WithTimeout(context.Background(), qr.TTL+time.Second)
	defer cancel()
	t := time.NewTimer(qr.TTL)
	defer t.Stop()
	select {
	case <-t.C:
		code, err := s.repo.Get(uuid)
		if err != nil || code.Status.IsTerminal() {
			return
		}
		updated, errU := s.repo.UpdateStatus(uuid, qr.StatusExpired)
		if errU != nil {
			return
		}
		go s.maybeWebhook(updated)
		log.Printf("[QR] auto-expired %s", uuid)
	case <-ctx.Done():
	}
}

func (s *Service) maybeWebhook(code *qr.Code) {
	if s.webhook == nil || !code.Status.IsTerminal() {
		return
	}
	_, _ = s.webhook.Send(code)
}

func shortID(uuid string) string {
	if len(uuid) > 8 {
		return uuid[:8]
	}
	return uuid
}

// GenerateInput — параметры запроса на генерацию QR.
type GenerateInput struct {
	BIN      string
	TID      string
	MID      string
	Amount   float64
	DataType string
}
