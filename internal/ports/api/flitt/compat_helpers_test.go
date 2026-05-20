package flitt_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vevovip/chaospay/internal/infrastructure/pgclient"
)

func httpTestServerCapturing(t *testing.T, ch chan<- []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		ch <- body
		w.WriteHeader(http.StatusOK)
	}))
}

func pgClientWithSecret(url, secret string) *pgclient.FlittClient {
	return pgclient.NewFlittClient(url, url+"/bind", secret)
}

func pgClientWithBindURL(bindURL, secret string) *pgclient.FlittClient {
	return pgclient.NewFlittClient("", bindURL, secret)
}

func timeout(seconds int) <-chan time.Time {
	return time.After(time.Duration(seconds) * time.Second)
}
