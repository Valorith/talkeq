package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/jbsmith7741/toml"
	"github.com/xackery/talkeq/config"
	"github.com/xackery/talkeq/tlog"
)

//go:embed static/*
var staticFiles embed.FS

// StatusProvider allows checking connection status of services
type StatusProvider interface {
	IsDiscordConnected() bool
	IsTelnetConnected() bool
	IsAPIConnected() bool
}

// Web represents the web dashboard service
type Web struct {
	ctx            context.Context
	cancel         context.CancelFunc
	config         config.Web
	fullConfig     *config.Config
	mutex          sync.RWMutex
	isConnected    bool
	statusProvider StatusProvider
	server         *http.Server
}

// New creates a new web dashboard
func New(ctx context.Context, cfg config.Web, fullConfig *config.Config, sp StatusProvider) (*Web, error) {
	ctx, cancel := context.WithCancel(ctx)
	w := &Web{
		ctx:            ctx,
		cancel:         cancel,
		config:         cfg,
		fullConfig:     fullConfig,
		statusProvider: sp,
	}
	return w, nil
}

// Connect starts the web dashboard server
func (w *Web) Connect(ctx context.Context) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if !w.config.IsEnabled {
		tlog.Debugf("[web] is disabled, skipping connect")
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", w.handleIndex)
	mux.HandleFunc("/api/config", w.handleConfig)
	mux.HandleFunc("/api/status", w.handleStatus)
	mux.HandleFunc("/api/config/save", w.handleConfigSave)

	w.server = &http.Server{
		Addr:    w.config.Host,
		Handler: mux,
	}

	tlog.Infof("[web] dashboard listening on %s...", w.config.Host)

	go func() {
		err := w.server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			tlog.Errorf("[web] server failed: %s", err)
		}
		w.mutex.Lock()
		w.isConnected = false
		w.mutex.Unlock()
	}()

	w.isConnected = true
	tlog.Infof("[web] dashboard started successfully")
	return nil
}

// IsConnected returns if the web dashboard is running
func (w *Web) IsConnected() bool {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.isConnected
}

// Disconnect stops the web dashboard
func (w *Web) Disconnect(ctx context.Context) error {
	if !w.config.IsEnabled {
		return nil
	}
	if !w.isConnected {
		return nil
	}
	if w.server != nil {
		w.server.Shutdown(ctx)
	}
	w.isConnected = false
	return nil
}

func (w *Web) handleIndex(rw http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(rw, "failed to load dashboard", http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Write(data)
}

// configResponse is the sanitized config sent to the browser (tokens masked)
type configResponse struct {
	Debug        bool   `json:"debug"`
	KeepAlive    bool   `json:"keep_alive"`
	KeepAliveRetry string `json:"keep_alive_retry"`

	DiscordEnabled  bool   `json:"discord_enabled"`
	DiscordToken    string `json:"discord_token"`
	DiscordServerID string `json:"discord_server_id"`
	DiscordClientID string `json:"discord_client_id"`
	DiscordBotStatus string `json:"discord_bot_status"`
	DiscordRouteCount int  `json:"discord_route_count"`

	TelnetEnabled bool   `json:"telnet_enabled"`
	TelnetHost    string `json:"telnet_host"`
	TelnetRouteCount int `json:"telnet_route_count"`

	APIEnabled bool   `json:"api_enabled"`
	APIHost    string `json:"api_host"`

	EQLogEnabled bool   `json:"eqlog_enabled"`
	EQLogPath    string `json:"eqlog_path"`

	SQLReportEnabled bool   `json:"sqlreport_enabled"`
	SQLReportHost    string `json:"sqlreport_host"`
	SQLReportDatabase string `json:"sqlreport_database"`

	WebEnabled bool   `json:"web_enabled"`
	WebHost    string `json:"web_host"`
}

func maskToken(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "..." + token[len(token)-4:]
}

func (w *Web) handleConfig(rw http.ResponseWriter, r *http.Request) {
	w.mutex.RLock()
	cfg := w.fullConfig
	w.mutex.RUnlock()

	resp := configResponse{
		Debug:          cfg.Debug,
		KeepAlive:      cfg.IsKeepAliveEnabled,
		KeepAliveRetry: cfg.KeepAliveRetry,

		DiscordEnabled:   cfg.Discord.IsEnabled,
		DiscordToken:     maskToken(cfg.Discord.Token),
		DiscordServerID:  cfg.Discord.ServerID,
		DiscordClientID:  cfg.Discord.ClientID,
		DiscordBotStatus: cfg.Discord.BotStatus,
		DiscordRouteCount: len(cfg.Discord.Routes),

		TelnetEnabled:    cfg.Telnet.IsEnabled,
		TelnetHost:       cfg.Telnet.Host,
		TelnetRouteCount: len(cfg.Telnet.Routes),

		APIEnabled: cfg.API.IsEnabled,
		APIHost:    cfg.API.Host,

		EQLogEnabled: cfg.EQLog.IsEnabled,
		EQLogPath:    cfg.EQLog.Path,

		SQLReportEnabled:  cfg.SQLReport.IsEnabled,
		SQLReportHost:     cfg.SQLReport.Host,
		SQLReportDatabase: cfg.SQLReport.Database,

		WebEnabled: cfg.Web.IsEnabled,
		WebHost:    cfg.Web.Host,
	}

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(resp)
}

type statusResponse struct {
	Discord bool `json:"discord"`
	Telnet  bool `json:"telnet"`
	API     bool `json:"api"`
	Web     bool `json:"web"`
}

func (w *Web) handleStatus(rw http.ResponseWriter, r *http.Request) {
	resp := statusResponse{
		Web: w.isConnected,
	}
	if w.statusProvider != nil {
		resp.Discord = w.statusProvider.IsDiscordConnected()
		resp.Telnet = w.statusProvider.IsTelnetConnected()
		resp.API = w.statusProvider.IsAPIConnected()
	}
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(resp)
}

type configSaveRequest struct {
	Debug          *bool   `json:"debug,omitempty"`
	KeepAlive      *bool   `json:"keep_alive,omitempty"`
	KeepAliveRetry *string `json:"keep_alive_retry,omitempty"`
	DiscordEnabled *bool   `json:"discord_enabled,omitempty"`
	TelnetEnabled  *bool   `json:"telnet_enabled,omitempty"`
	TelnetHost     *string `json:"telnet_host,omitempty"`
	APIEnabled     *bool   `json:"api_enabled,omitempty"`
	APIHost        *string `json:"api_host,omitempty"`
	WebEnabled     *bool   `json:"web_enabled,omitempty"`
	WebHost        *string `json:"web_host,omitempty"`
}

func (w *Web) handleConfigSave(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(rw, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req configSaveRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(rw, "invalid json", http.StatusBadRequest)
		return
	}

	w.mutex.Lock()
	cfg := w.fullConfig

	if req.Debug != nil {
		cfg.Debug = *req.Debug
	}
	if req.KeepAlive != nil {
		cfg.IsKeepAliveEnabled = *req.KeepAlive
	}
	if req.KeepAliveRetry != nil {
		cfg.KeepAliveRetry = *req.KeepAliveRetry
	}
	if req.DiscordEnabled != nil {
		cfg.Discord.IsEnabled = *req.DiscordEnabled
	}
	if req.TelnetEnabled != nil {
		cfg.Telnet.IsEnabled = *req.TelnetEnabled
	}
	if req.TelnetHost != nil {
		cfg.Telnet.Host = *req.TelnetHost
	}
	if req.APIEnabled != nil {
		cfg.API.IsEnabled = *req.APIEnabled
	}
	if req.APIHost != nil {
		cfg.API.Host = *req.APIHost
	}
	if req.WebEnabled != nil {
		cfg.Web.IsEnabled = *req.WebEnabled
	}
	if req.WebHost != nil {
		cfg.Web.Host = *req.WebHost
	}
	w.mutex.Unlock()

	// Write config to file
	f, err := os.Create("talkeq.conf")
	if err != nil {
		http.Error(rw, fmt.Sprintf("failed to open config: %s", err), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	enc := toml.NewEncoder(f)
	if err := enc.Encode(cfg); err != nil {
		http.Error(rw, fmt.Sprintf("failed to write config: %s", err), http.StatusInternalServerError)
		return
	}

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]string{"status": "ok", "message": "Config saved. Restart TalkEQ for changes to take effect."})
}
