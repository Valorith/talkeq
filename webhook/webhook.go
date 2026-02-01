package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/xackery/talkeq/config"
	"github.com/xackery/talkeq/request"
	"github.com/xackery/talkeq/tlog"
)

// SendRequest is the JSON body for POST /api/send
type SendRequest struct {
	Channel string `json:"channel"`
	Message string `json:"message"`
	Sender  string `json:"sender"`
}

// SendResponse is returned from POST /api/send
type SendResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// channelCommands maps channel names to their telnet command format.
// These use the EQEmu telnet world command syntax.
var channelCommands = map[string]string{
	"ooc":       "emote world 260 %s says ooc, '%s'",
	"auction":   "emote world 261 %s auctions, '%s'",
	"shout":     "emote world 262 %s shouts, '%s'",
	"guild":     "emote world 259 %s says to the guild, '%s'",
	"broadcast": "worldbroadcast %s: %s",
}

// Webhook represents the webhook HTTP server
type Webhook struct {
	ctx         context.Context
	cancel      context.CancelFunc
	isConnected bool
	mu          sync.RWMutex
	config      config.Webhook
	subscribers []func(interface{}) error
	server      *http.Server
}

// New creates a new webhook server
func New(ctx context.Context, cfg config.Webhook) (*Webhook, error) {
	ctx, cancel := context.WithCancel(ctx)
	w := &Webhook{
		ctx:    ctx,
		config: cfg,
		cancel: cancel,
	}

	if !cfg.IsEnabled {
		return w, nil
	}

	return w, nil
}

// Subscribe registers a message handler
func (w *Webhook) Subscribe(ctx context.Context, onMessage func(interface{}) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.subscribers = append(w.subscribers, onMessage)
	return nil
}

// Connect starts the webhook HTTP server
func (w *Webhook) Connect(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.config.IsEnabled {
		tlog.Debugf("[webhook] is disabled, skipping connect")
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/send", w.handleSend)
	mux.HandleFunc("/api/channels", w.handleChannels)
	mux.HandleFunc("/health", w.handleHealth)

	w.server = &http.Server{
		Addr:    w.config.Host,
		Handler: mux,
	}

	tlog.Infof("[webhook] listening on %s...", w.config.Host)

	go func() {
		err := w.server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			tlog.Errorf("[webhook] server error: %s", err)
		}
		w.mu.Lock()
		w.isConnected = false
		w.mu.Unlock()
	}()

	w.isConnected = true
	tlog.Infof("[webhook] started successfully")
	return nil
}

// IsConnected returns if the server is running
func (w *Webhook) IsConnected() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.isConnected
}

// Disconnect stops the webhook server
func (w *Webhook) Disconnect(ctx context.Context) error {
	if !w.config.IsEnabled {
		return nil
	}
	if !w.isConnected {
		return nil
	}
	if w.server != nil {
		err := w.server.Shutdown(ctx)
		if err != nil {
			tlog.Warnf("[webhook] shutdown error: %s", err)
		}
	}
	w.isConnected = false
	return nil
}

func (w *Webhook) authenticate(r *http.Request) bool {
	if w.config.Token == "" {
		return true
	}
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return false
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return false
	}
	return parts[1] == w.config.Token
}

func (w *Webhook) writeJSON(rw http.ResponseWriter, status int, resp SendResponse) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	json.NewEncoder(rw).Encode(resp)
}

func (w *Webhook) handleSend(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.writeJSON(rw, http.StatusMethodNotAllowed, SendResponse{Error: "method not allowed"})
		return
	}

	if !w.authenticate(r) {
		w.writeJSON(rw, http.StatusUnauthorized, SendResponse{Error: "unauthorized"})
		return
	}

	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.writeJSON(rw, http.StatusBadRequest, SendResponse{Error: "invalid JSON: " + err.Error()})
		return
	}

	if req.Channel == "" {
		w.writeJSON(rw, http.StatusBadRequest, SendResponse{Error: "channel is required"})
		return
	}
	if req.Message == "" {
		w.writeJSON(rw, http.StatusBadRequest, SendResponse{Error: "message is required"})
		return
	}
	if req.Sender == "" {
		w.writeJSON(rw, http.StatusBadRequest, SendResponse{Error: "sender is required"})
		return
	}

	channelKey := strings.ToLower(req.Channel)
	cmdFmt, ok := channelCommands[channelKey]
	if !ok {
		supported := make([]string, 0, len(channelCommands))
		for k := range channelCommands {
			supported = append(supported, k)
		}
		w.writeJSON(rw, http.StatusBadRequest, SendResponse{
			Error: fmt.Sprintf("unsupported channel '%s', supported: %s", req.Channel, strings.Join(supported, ", ")),
		})
		return
	}

	telnetMsg := fmt.Sprintf(cmdFmt, req.Sender, req.Message)

	tlog.Infof("[webhook] sending to %s: %s", channelKey, telnetMsg)

	telnetReq := request.TelnetSend{
		Ctx:     context.Background(),
		Message: telnetMsg,
	}

	w.mu.RLock()
	subscribers := w.subscribers
	w.mu.RUnlock()

	var lastErr error
	sent := false
	for i, s := range subscribers {
		err := s(telnetReq)
		if err != nil {
			tlog.Warnf("[webhook->telnet subscriber %d] failed: %s", i, err)
			lastErr = err
			continue
		}
		sent = true
		tlog.Infof("[webhook->telnet subscriber %d] sent: %s", i, telnetMsg)
	}

	if !sent {
		errMsg := "no subscribers available"
		if lastErr != nil {
			errMsg = lastErr.Error()
		}
		w.writeJSON(rw, http.StatusServiceUnavailable, SendResponse{Error: errMsg})
		return
	}

	w.writeJSON(rw, http.StatusOK, SendResponse{Success: true})
}

func (w *Webhook) handleChannels(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.writeJSON(rw, http.StatusMethodNotAllowed, SendResponse{Error: "method not allowed"})
		return
	}

	if !w.authenticate(r) {
		w.writeJSON(rw, http.StatusUnauthorized, SendResponse{Error: "unauthorized"})
		return
	}

	channels := make([]string, 0, len(channelCommands))
	for k := range channelCommands {
		channels = append(channels, k)
	}

	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]interface{}{
		"channels": channels,
	})
}

func (w *Webhook) handleHealth(rw http.ResponseWriter, r *http.Request) {
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]interface{}{
		"status": "ok",
	})
}
