// Package qrgen генерирует PNG-картинки QR-кодов в base64.
package qrgen

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	qrcode "github.com/skip2/go-qrcode"
)

// Generator — конфигурация QR-генератора.
type Generator struct {
	hostBase string
}

// NewGenerator конструктор. hostBase — куда «ведёт» QR (например, https://example.com/pay/).
func NewGenerator(hostBase string) *Generator {
	if hostBase == "" {
		hostBase = "https://example.com/pay/"
	}
	return &Generator{hostBase: hostBase}
}

// PaymentURL — URL, кодируемый в QR-картинку.
func (g *Generator) PaymentURL(uuid string) string {
	return fmt.Sprintf("%s%s", g.hostBase, uuid)
}

// Generate возвращает PNG-картинку в base64.
func (g *Generator) Generate(content string) (string, error) {
	png, err := qrcode.Encode(content, qrcode.Medium, 256)
	if err != nil {
		return "", fmt.Errorf("encode qr: %w", err)
	}
	return base64.StdEncoding.EncodeToString(png), nil
}

// GenerateUUID — короткий случайный UUID-like ID (8 байт hex).
func GenerateUUID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
