package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

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

// rateLimiter tracks request timestamps per action
type rateLimiter struct {
	mu       sync.Mutex
	requests []time.Time
	max      int
	window   time.Duration
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{max: max, window: window}
}

func (rl *rateLimiter) allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-rl.window)
	// Prune old entries
	valid := rl.requests[:0]
	for _, t := range rl.requests {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	rl.requests = valid
	if len(rl.requests) >= rl.max {
		return false
	}
	rl.requests = append(rl.requests, now)
	return true
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
	csrfToken      string
	saveLimiter    *rateLimiter
}

// New creates a new web dashboard
func New(ctx context.Context, cfg config.Web, fullConfig *config.Config, sp StatusProvider) (*Web, error) {
	ctx, cancel := context.WithCancel(ctx)

	// Generate CSRF token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		cancel()
		return nil, fmt.Errorf("generate csrf token: %w", err)
	}

	w := &Web{
		ctx:            ctx,
		cancel:         cancel,
		config:         cfg,
		fullConfig:     fullConfig,
		statusProvider: sp,
		csrfToken:      hex.EncodeToString(tokenBytes),
		saveLimiter:    newRateLimiter(5, time.Minute),
	}
	return w, nil
}

// securityHeaders adds security headers to every response
func securityHeaders(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self'; connect-src 'self'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next(w, r)
	}
}

// basicAuth wraps a handler with optional HTTP Basic Auth
func (web *Web) basicAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if web.config.Username == "" && web.config.Password == "" {
			next(w, r)
			return
		}

		user, pass, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="TalkEQ Dashboard"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		userMatch := subtle.ConstantTimeCompare([]byte(user), []byte(web.config.Username))
		passMatch := subtle.ConstantTimeCompare([]byte(pass), []byte(web.config.Password))
		if userMatch&passMatch != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="TalkEQ Dashboard"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
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
	mux.HandleFunc("/", securityHeaders(w.basicAuth(w.handleIndex)))
	mux.HandleFunc("/api/config", securityHeaders(w.basicAuth(w.handleConfig)))
	mux.HandleFunc("/api/status", securityHeaders(w.basicAuth(w.handleStatus)))
	mux.HandleFunc("/api/csrf-token", securityHeaders(w.basicAuth(w.handleCSRFToken)))
	mux.HandleFunc("/api/config/save", securityHeaders(w.basicAuth(w.handleConfigSave)))

	w.server = &http.Server{
		Addr:         w.config.Host,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
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

// sanitize escapes a string for safe HTML rendering
func sanitize(s string) string {
	return html.EscapeString(s)
}

func (w *Web) handleConfig(rw http.ResponseWriter, r *http.Request) {
	w.mutex.RLock()
	cfg := w.fullConfig
	w.mutex.RUnlock()

	resp := configResponse{
		Debug:          cfg.Debug,
		KeepAlive:      cfg.IsKeepAliveEnabled,
		KeepAliveRetry: sanitize(cfg.KeepAliveRetry),

		DiscordEnabled:   cfg.Discord.IsEnabled,
		DiscordToken:     maskToken(cfg.Discord.Token),
		DiscordServerID:  sanitize(cfg.Discord.ServerID),
		DiscordClientID:  sanitize(cfg.Discord.ClientID),
		DiscordBotStatus: sanitize(cfg.Discord.BotStatus),
		DiscordRouteCount: len(cfg.Discord.Routes),

		TelnetEnabled:    cfg.Telnet.IsEnabled,
		TelnetHost:       sanitize(cfg.Telnet.Host),
		TelnetRouteCount: len(cfg.Telnet.Routes),

		APIEnabled: cfg.API.IsEnabled,
		APIHost:    sanitize(cfg.API.Host),

		EQLogEnabled: cfg.EQLog.IsEnabled,
		EQLogPath:    sanitize(cfg.EQLog.Path),

		SQLReportEnabled:  cfg.SQLReport.IsEnabled,
		SQLReportHost:     sanitize(cfg.SQLReport.Host),
		SQLReportDatabase: sanitize(cfg.SQLReport.Database),

		WebEnabled: cfg.Web.IsEnabled,
		WebHost:    sanitize(cfg.Web.Host),
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

func (w *Web) handleCSRFToken(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]string{"token": w.csrfToken})
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

	// Rate limiting
	if !w.saveLimiter.allow() {
		http.Error(rw, "rate limit exceeded, max 5 saves per minute", http.StatusTooManyRequests)
		return
	}

	// CSRF check: require X-CSRF-Token header
	csrfHeader := r.Header.Get("X-CSRF-Token")
	if subtle.ConstantTimeCompare([]byte(csrfHeader), []byte(w.csrfToken)) != 1 {
		http.Error(rw, "invalid or missing CSRF token", http.StatusForbidden)
		return
	}

	// Limit body size at the stream level to prevent memory exhaustion
	r.Body = http.MaxBytesReader(rw, r.Body, 1024*64)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(rw, "request body too large", http.StatusRequestEntityTooLarge)
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
		cfg.KeepAliveRetry = sanitizeConfigValue(*req.KeepAliveRetry)
	}
	if req.DiscordEnabled != nil {
		cfg.Discord.IsEnabled = *req.DiscordEnabled
	}
	if req.TelnetEnabled != nil {
		cfg.Telnet.IsEnabled = *req.TelnetEnabled
	}
	if req.TelnetHost != nil {
		cfg.Telnet.Host = sanitizeConfigValue(*req.TelnetHost)
	}
	if req.APIEnabled != nil {
		cfg.API.IsEnabled = *req.APIEnabled
	}
	if req.APIHost != nil {
		cfg.API.Host = sanitizeConfigValue(*req.APIHost)
	}
	if req.WebEnabled != nil {
		cfg.Web.IsEnabled = *req.WebEnabled
	}
	if req.WebHost != nil {
		cfg.Web.Host = sanitizeConfigValue(*req.WebHost)
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

	// Rotate CSRF token after successful save
	newTokenBytes := make([]byte, 32)
	if _, err := rand.Read(newTokenBytes); err != nil {
		http.Error(rw, "failed to generate new csrf token", http.StatusInternalServerError)
		return
	}
	w.mutex.Lock()
	w.csrfToken = hex.EncodeToString(newTokenBytes)
	newToken := w.csrfToken
	w.mutex.Unlock()

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]string{"status": "ok", "message": "Config saved. Restart TalkEQ for changes to take effect.", "csrf_token": newToken})
}

// sanitizeConfigValue strips control characters and trims whitespace from user-provided config values
func sanitizeConfigValue(s string) string {
	s = strings.TrimSpace(s)
	// Remove any control characters
	var b strings.Builder
	for _, r := range s {
		if r >= 32 && r != 127 {
			b.WriteRune(r)
		}
	}
	return b.String()
}
