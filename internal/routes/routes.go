package routes

import (
	"context"
	"embed"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"amnezia-manager-bot/internal/patcher"
)

//go:embed assets/allowed_ips_default.txt
var defaultFS embed.FS

// Service хранит last-known-good список AllowedIPs для split-routing.
// Начальное значение — встроенный файл, далее обновляется из URL.
type Service struct {
	url    string
	client *http.Client
	log    *slog.Logger

	mu   sync.RWMutex
	list []string
}

func New(url string, log *slog.Logger) (*Service, error) {
	raw, err := defaultFS.ReadFile("assets/allowed_ips_default.txt")
	if err != nil {
		return nil, fmt.Errorf("read embedded default list: %w", err)
	}
	list, err := patcher.ValidateAndClean(parseFile(raw))
	if err != nil {
		return nil, fmt.Errorf("embedded default list invalid: %w", err)
	}
	return &Service{
		url:    url,
		client: &http.Client{Timeout: 30 * time.Second},
		log:    log,
		list:   list,
	}, nil
}

func parseFile(b []byte) []string {
	s := strings.TrimSpace(string(b))
	if strings.HasPrefix(s, "AllowedIPs") {
		if i := strings.IndexByte(s, '='); i >= 0 {
			s = s[i+1:]
		}
	}
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	})
}

// AllowedIPs возвращает текущий last-known-good список (никогда не пустой).
func (s *Service) AllowedIPs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.list...)
}

// Refresh скачивает и валидирует список; при ошибке текущий список не меняется.
func (s *Service) Refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	list, err := patcher.ValidateAndClean(parseFile(body))
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	s.mu.Lock()
	s.list = list
	s.mu.Unlock()
	return nil
}

// Run периодически обновляет список до отмены ctx.
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := s.Refresh(ctx); err != nil {
			s.log.Warn("routes refresh failed, keeping last-known-good", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
