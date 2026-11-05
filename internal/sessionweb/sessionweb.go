// Package sessionweb serves a small local web page for looking at and editing
// the messages of Crush's sessions: the chat history that is re-sent to the
// model on every turn. It exists so the history can be changed by clicking
// and typing, and so what the model receives can be seen in one place.
package sessionweb

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
)

//go:embed page.html
var pageHTML embed.FS

// Store is the slice of Crush's session and message services the page needs.
type Store interface {
	ListSessions(ctx context.Context) ([]session.Session, error)
	ListMessages(ctx context.Context, sessionID string) ([]message.Message, error)
	GetMessage(ctx context.Context, id string) (message.Message, error)
	UpdateMessage(ctx context.Context, msg message.Message) error
	DeleteMessage(ctx context.Context, id string) error
	CreateMessage(ctx context.Context, sessionID string, params message.CreateMessageParams) (message.Message, error)
}

// Context is what the model receives besides the history: the prompt
// template in effect and the tool descriptions. Nil fields are allowed.
type Context struct {
	Template       string             `json:"template"`
	TemplateSource string             `json:"template_source"`
	Tools          []fantasy.ToolInfo `json:"tools"`
}

// ContextFunc builds the Context for the current project; may be nil.
type ContextFunc func(ctx context.Context) (Context, error)

// Server is the HTTP handler.
type Server struct {
	store   Store
	context ContextFunc
	mux     *http.ServeMux
}

// New wires the routes.
func New(store Store, contextFn ContextFunc) *Server {
	s := &Server{store: store, context: contextFn, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /{$}", s.page)
	s.mux.HandleFunc("GET /api/sessions", s.listSessions)
	s.mux.HandleFunc("GET /api/sessions/{id}/messages", s.listMessages)
	s.mux.HandleFunc("POST /api/sessions/{id}/messages", s.createMessage)
	s.mux.HandleFunc("PUT /api/messages/{id}", s.updateMessage)
	s.mux.HandleFunc("DELETE /api/messages/{id}", s.deleteMessage)
	s.mux.HandleFunc("GET /api/context", s.getContext)
	return s
}

// ServeHTTP refuses the three ways another site can reach this server
// through the user's browser: a Host that is not a loopback name, which is
// what a DNS-rebound name pointing at 127.0.0.1 sends; a request the browser
// marks as coming from somewhere else, which is the only sign a frame or a
// navigation gives, since neither carries an Origin; and an Origin that is
// not the Host it was sent to. A write with no Origin is refused too: a
// browser always sends one, so the allowance below is for reads by a client
// that is not a browser, and a write from one has to name its Origin.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackHost(r.Host) {
		http.Error(w, "host not allowed", http.StatusForbidden)
		return
	}
	// Sec-Fetch-Site is a forbidden header name, so a page cannot set it, and
	// a browser sends it on everything, a frame and a top-level navigation
	// included: none for a typed URL or a bookmark, same-origin for the
	// page's own requests. A client that is not a browser sends nothing here.
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		http.Error(w, "cross-site request not allowed", http.StatusForbidden)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
	} else if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "a write needs an Origin naming the Host", http.StatusForbidden)
		return
	}
	s.mux.ServeHTTP(w, r)
}

// isLoopbackHost reports whether hostport names this machine. A Host header
// is case-insensitive and localhost. is the same name fully qualified, so
// both are normalised away first; the comparison itself stays exact, because
// evil.localhost is another name entirely.
func isLoopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return ip.IsLoopback()
	}
	return isShorthandLoopbackIPv4(host)
}

// isShorthandLoopbackIPv4 reports whether host is one of the shorter dotted
// decimal spellings of an address in 127.0.0.0/8, such as 127.1 or 127.0.1.
// net.ParseIP takes the four field form alone, while curl and the C library
// read the shorter ones, so without this they are read as names and refused.
// Only the first field decides the /8, and a name an attacker can register is
// never all digits and dots.
func isShorthandLoopbackIPv4(host string) bool {
	fields := strings.Split(host, ".")
	if len(fields) < 2 || len(fields) > 3 {
		return false
	}
	for _, f := range fields {
		if f == "" {
			return false
		}
		for i := 0; i < len(f); i++ {
			if f[i] < '0' || f[i] > '9' {
				return false
			}
		}
	}
	return fields[0] == "127"
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	b, err := pageHTML.ReadFile("page.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// For a browser too old to send Sec-Fetch-Site, which ServeHTTP would
	// then have nothing to refuse the frame by. Framing the editor is what
	// puts it under another site's clicks.
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

type sessionJSON struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	MessageCount int64  `json:"message_count"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

type messageJSON struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	Role      string          `json:"role"`
	Model     string          `json:"model"`
	Provider  string          `json:"provider"`
	CreatedAt int64           `json:"created_at"`
	IsSummary bool            `json:"is_summary"`
	Parts     json.RawMessage `json:"parts"`
}

func toMessageJSON(m message.Message) (messageJSON, error) {
	parts, err := message.MarshalParts(m.Parts)
	if err != nil {
		return messageJSON{}, err
	}
	return messageJSON{
		ID: m.ID, SessionID: m.SessionID, Role: string(m.Role), Model: m.Model, Provider: m.Provider,
		CreatedAt: m.CreatedAt, IsSummary: m.IsSummaryMessage, Parts: parts,
	}, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.ListSessions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]sessionJSON, 0, len(sessions))
	for _, se := range sessions {
		out = append(out, sessionJSON{ID: se.ID, Title: se.Title, MessageCount: se.MessageCount, CreatedAt: se.CreatedAt, UpdatedAt: se.UpdatedAt})
	}
	writeJSON(w, out)
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	msgs, err := s.store.ListMessages(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]messageJSON, 0, len(msgs))
	for _, m := range msgs {
		mj, err := toMessageJSON(m)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, mj)
	}
	writeJSON(w, out)
}

func (s *Server) createMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role string `json:"role"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	role := message.MessageRole(in.Role)
	if role != message.User && role != message.Assistant {
		http.Error(w, "role must be user or assistant", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		http.Error(w, "text is empty", http.StatusBadRequest)
		return
	}
	m, err := s.store.CreateMessage(r.Context(), r.PathValue("id"), message.CreateMessageParams{
		Role:  role,
		Parts: []message.ContentPart{message.TextContent{Text: in.Text}, message.Finish{Reason: message.FinishReasonEndTurn, Time: time.Now().Unix()}},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	mj, err := toMessageJSON(m)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, mj)
}

func (s *Server) updateMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Parts json.RawMessage `json:"parts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	parts, err := message.UnmarshalParts(in.Parts)
	if err != nil {
		http.Error(w, fmt.Sprintf("parts: %v", err), http.StatusBadRequest)
		return
	}
	if len(parts) == 0 {
		http.Error(w, "a message needs at least one part; delete it instead", http.StatusBadRequest)
		return
	}
	m, err := s.store.GetMessage(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	m.Parts = parts
	if err := s.store.UpdateMessage(r.Context(), m); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteMessage(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteMessage(r.Context(), r.PathValue("id")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getContext(w http.ResponseWriter, r *http.Request) {
	if s.context == nil {
		http.Error(w, "no context available", http.StatusNotFound)
		return
	}
	c, err := s.context(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, c)
}

// ErrNotFound is returned by a Store for an unknown id.
var ErrNotFound = errors.New("not found")
