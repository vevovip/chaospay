// Package kaspi — use-cases мока KaspiPay.
package kaspi

import (
	domainkaspi "github.com/vevovip/chaospay/internal/domain/kaspi"
)

// Repo — хранилище Kaspi-платежей.
type Repo interface {
	Create(externalID string, amount float64) *domainkaspi.Payment
	Get(paymentID int) (*domainkaspi.Payment, error)
	List() []*domainkaspi.Payment
	SetStatus(paymentID int, status domainkaspi.Status) (*domainkaspi.Payment, error)
}

// BehaviorOptions — PaymentBehaviorOptions, которые банк отдаёт в create-link
// и по которым PG строит цикл поллинга статуса.
type BehaviorOptions struct {
	StatusPollingInterval      int
	LinkActivationWaitTimeout  int
	PaymentConfirmationTimeout int
}

// Service — сервис мока KaspiPay.
type Service struct {
	repo     Repo
	behavior BehaviorOptions
}

// NewService конструктор.
func NewService(repo Repo, behavior BehaviorOptions) *Service {
	return &Service{repo: repo, behavior: behavior}
}

// Behavior возвращает настройки поведения поллинга.
func (s *Service) Behavior() BehaviorOptions {
	return s.behavior
}

// CreateLink создаёт платёж (статус Wait) и возвращает его.
func (s *Service) CreateLink(externalID string, amount float64) *domainkaspi.Payment {
	return s.repo.Create(externalID, amount)
}

// GetStatus возвращает текущее состояние платежа.
func (s *Service) GetStatus(paymentID int) (*domainkaspi.Payment, error) {
	return s.repo.Get(paymentID)
}

// List возвращает все платежи (для панели).
func (s *Service) List() []*domainkaspi.Payment {
	return s.repo.List()
}

// SetStatus меняет статус платежа (confirm/decline из тестовых эндпоинтов).
func (s *Service) SetStatus(paymentID int, status domainkaspi.Status) (*domainkaspi.Payment, error) {
	return s.repo.SetStatus(paymentID, status)
}
