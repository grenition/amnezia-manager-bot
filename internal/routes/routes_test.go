package routes

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEmbeddedDefault(t *testing.T) {
	s, err := New("http://unused", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if list := s.AllowedIPs(); len(list) < 100 {
		t.Fatalf("embedded list too small: %d", len(list))
	}
}

func TestParseFile(t *testing.T) {
	cases := map[string][]string{
		"AllowedIPs = 1.0.0.0/8, 2.0.0.0/7": {"1.0.0.0/8", "2.0.0.0/7"},
		"1.0.0.0/8,2.0.0.0/7":               {"1.0.0.0/8", "2.0.0.0/7"},
		"1.0.0.0/8\n2.0.0.0/7\n":            {"1.0.0.0/8", "2.0.0.0/7"},
	}
	for in, want := range cases {
		if got := parseFile([]byte(in)); strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("parseFile(%q) = %v", in, got)
		}
	}
}

func TestRefreshOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("AllowedIPs = 5.0.0.0/8, 6.0.0.0/8"))
	}))
	defer srv.Close()
	s, err := New(srv.URL, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.AllowedIPs(), ","); got != "5.0.0.0/8,6.0.0.0/8" {
		t.Fatalf("got %q", got)
	}
}

func TestRefreshKeepsLastKnownGood(t *testing.T) {
	s, err := New("http://unused", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	before := strings.Join(s.AllowedIPs(), ",")

	s.url = "http://127.0.0.1:1/unreachable"
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if after := strings.Join(s.AllowedIPs(), ","); after != before {
		t.Fatal("list must be unchanged on fetch failure")
	}

	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv500.Close()
	s.url = srv500.URL
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatal("expected error on 500")
	}
	if after := strings.Join(s.AllowedIPs(), ","); after != before {
		t.Fatal("list must be unchanged on 500")
	}

	srvBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("AllowedIPs = 0.0.0.0/0"))
	}))
	defer srvBad.Close()
	s.url = srvBad.URL
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatal("expected error on invalid list")
	}
	if after := strings.Join(s.AllowedIPs(), ","); after != before {
		t.Fatal("list must be unchanged on invalid list")
	}
}

func TestRunRefreshesPeriodically(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte("AllowedIPs = 5.0.0.0/8"))
	}))
	defer srv.Close()
	s, err := New(srv.URL, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	go s.Run(ctx, 50*time.Millisecond)
	<-ctx.Done()
	if calls.Load() < 2 {
		t.Fatalf("expected periodic refreshes, calls=%d", calls.Load())
	}
}
