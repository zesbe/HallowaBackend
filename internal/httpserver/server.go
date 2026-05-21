package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/zesbe/Sebar/internal/whatsapp"
)

type Server struct {
	mgr            *whatsapp.Manager
	internalAPIKey string
	log            *slog.Logger
	mux            *http.ServeMux
}

func New(mgr *whatsapp.Manager, internalAPIKey string, log *slog.Logger) *Server {
	s := &Server{
		mgr:            mgr,
		internalAPIKey: internalAPIKey,
		log:            log,
		mux:            http.NewServeMux(),
	}
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/send-message", s.handleSendMessage)
	return s
}

func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.log.Info("http listening", "addr", addr)
	return srv.ListenAndServe()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"active":    s.mgr.ActiveCount(),
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	})
}

type sendMessageReq struct {
	DeviceID string `json:"device_id"`
	To       string `json:"to"`       // e.g. "62812xxxxx" or full JID "62812xxxxx@s.whatsapp.net"
	Text     string `json:"text"`
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if !s.authOK(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	var req sendMessageReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json: " + err.Error()})
		return
	}
	if req.DeviceID == "" || req.To == "" || req.Text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "device_id, to, text required"})
		return
	}
	jid := req.To
	if !strings.Contains(jid, "@") {
		jid = strings.TrimPrefix(jid, "+") + "@s.whatsapp.net"
	}
	id, err := s.mgr.SendText(r.Context(), req.DeviceID, jid, req.Text)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "sent"})
}

func (s *Server) authOK(r *http.Request) bool {
	if s.internalAPIKey == "" {
		return true // no key configured = open (matches Node behaviour)
	}
	hdr := r.Header.Get("X-API-Key")
	if hdr == "" {
		hdr = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	return hdr == s.internalAPIKey
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
