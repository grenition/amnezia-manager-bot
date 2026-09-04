package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"amnezia-manager-bot/internal/alerts"
	"amnezia-manager-bot/internal/config"
	"amnezia-manager-bot/internal/db"
	"amnezia-manager-bot/internal/monitor"
	"amnezia-manager-bot/internal/routes"
	"amnezia-manager-bot/internal/service"
	"amnezia-manager-bot/internal/store/postgres"
	tgbot "amnezia-manager-bot/internal/tgbot"
	"amnezia-manager-bot/internal/vpn/sshprovider"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(pool); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	st := postgres.New(pool)
	vpnProv := sshprovider.New(cfg, log)

	routesSvc, err := routes.New(cfg.Routes.URL, log)
	if err != nil {
		return err
	}
	go routesSvc.Run(ctx, cfg.Routes.RefreshInterval)

	api, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		return fmt.Errorf("telegram api: %w", err)
	}

	names := map[string]string{}
	for _, s := range cfg.Servers {
		names[s.ID] = s.DisplayName
	}

	alertsMgr := alerts.NewManager(st, tgbot.NewSender(api), names, cfg.AdminIDs)
	svc := service.New(cfg, st, vpnProv, routesSvc, log)
	mon := monitor.New(vpnProv, alertsMgr, cfg.EnabledServers(), cfg.Monitor.CheckInterval, cfg.Monitor.DownThreshold, log)
	go mon.Run(ctx)

	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := pool.Ping(pingCtx); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})
		srv := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		if err := srv.ListenAndServe(); err != nil {
			log.Error("health server stopped", "err", err)
		}
	}()

	b := tgbot.New(api, svc, alertsMgr, log, names)
	return b.Run(ctx)
}
