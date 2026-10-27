package api

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"csgclaw/internal/agentengine"
	"csgclaw/internal/config"
	"git-devops.opencsg.com/product/agentichub/agentx/sdk/go/worker"
	"github.com/go-chi/chi/v5"
)

// This opt-in development adapter deliberately uses the public execution SDK.
// It contains no NATS client and never calls another CSGClaw instance.
//
//go:embed agentx_dev.html agentx_dev.js
var agentxDevAssets embed.FS

type agentxDevBinding struct {
	RemoteID string `json:"remote_id"`
	SourceID string `json:"source_id"`
}
type agentxDevAdapter struct {
	h                      *Handler
	base, token, statePath string
	ctx                    context.Context
	cancel                 context.CancelFunc
	mu                     sync.Mutex
	bindings               map[string]agentxDevBinding
	workers                map[string]context.CancelFunc
}

func (h *Handler) registerAgentXDevRoutes(router chi.Router) {
	if os.Getenv("CSGCLAW_AGENTX_DEV_PANEL") != "1" {
		return
	}
	base := strings.TrimRight(os.Getenv("CSGCLAW_AGENTX_URL"), "/")
	token := os.Getenv("CSGCLAW_AGENTX_TOKEN")
	parsed, e := url.Parse(base)
	if e != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || token == "" {
		return
	}
	root, e := config.DefaultDir()
	if e != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &agentxDevAdapter{h: h, base: base, token: token, ctx: ctx, cancel: cancel, statePath: filepath.Join(root, "agentx-dev-bindings.json"), bindings: map[string]agentxDevBinding{}, workers: map[string]context.CancelFunc{}}
	if data, e := os.ReadFile(d.statePath); e == nil {
		_ = json.Unmarshal(data, &d.bindings)
	}
	if h.eventStreamShutdown != nil {
		go func() { <-h.eventStreamShutdown; cancel() }()
	}
	router.Get("/agentx-dev", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		b, _ := agentxDevAssets.ReadFile("agentx_dev.html")
		_, _ = w.Write(b)
	})
	router.Get("/agentx-dev/app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "no-store")
		b, _ := agentxDevAssets.ReadFile("agentx_dev.js")
		_, _ = w.Write(b)
	})
	router.Route("/api/v1/agentx-dev", func(r chi.Router) {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !h.validateServerAccessToken(r.Header.Get("Authorization")) {
					writeCodedAPIError(w, 401, "unauthorized", "Local server authentication required")
					return
				}
				next.ServeHTTP(w, r)
			})
		})
		r.Get("/info", func(w http.ResponseWriter, r *http.Request) {
			d.mu.Lock()
			bindings := []agentxDevBinding{}
			for _, b := range d.bindings {
				bindings = append(bindings, b)
			}
			d.mu.Unlock()
			writeJSON(w, 200, map[string]any{"agentx_url": base, "local_origin": r.Host, "bindings": bindings})
		})
		r.Get("/local-agents", func(w http.ResponseWriter, r *http.Request) {
			agents, e := h.agentEngine.Agents().List(r.Context(), agentengine.AgentListOptions{})
			if e != nil {
				writeCodedAPIError(w, 500, "agent_list_failed", e.Error())
				return
			}
			items := []map[string]any{}
			for _, a := range agents {
				items = append(items, map[string]any{"id": a.ID, "name": a.Spec.Name, "state": a.Status.State, "ready": a.Status.Ready, "runtime": a.Spec.Runtime.Adapter})
			}
			writeJSON(w, 200, items)
		})
		r.Post("/publish", d.publish)
		r.Get("/agents", func(w http.ResponseWriter, r *http.Request) { d.proxy(w, r, "/v1/agents?"+r.URL.RawQuery) })
		r.Get("/agents/{id}", func(w http.ResponseWriter, r *http.Request) {
			d.proxy(w, r, "/v1/agents/"+url.PathEscape(chi.URLParam(r, "id")))
		})
		r.Delete("/agents/{id}", func(w http.ResponseWriter, r *http.Request) {
			d.proxy(w, r, "/v1/agents/"+url.PathEscape(chi.URLParam(r, "id")))
		})
		r.Post("/connections/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			d.mu.Lock()
			stop := d.workers[id]
			delete(d.workers, id)
			d.mu.Unlock()
			if stop != nil {
				stop()
			}
			w.WriteHeader(204)
		})
		r.Post("/connections/{id}/start", func(w http.ResponseWriter, r *http.Request) {
			id := chi.URLParam(r, "id")
			d.mu.Lock()
			binding, ok := d.bindings[id]
			d.mu.Unlock()
			if !ok {
				writeCodedAPIError(w, 404, "binding_not_found", "No local binding")
				return
			}
			d.start(binding)
			w.WriteHeader(204)
		})
		r.Post("/responses", func(w http.ResponseWriter, r *http.Request) { d.proxy(w, r, "/v1/responses") })
		r.Get("/responses/{id}", func(w http.ResponseWriter, r *http.Request) {
			d.proxy(w, r, "/v1/responses/"+url.PathEscape(chi.URLParam(r, "id")))
		})
		r.Post("/responses/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
			d.proxy(w, r, "/v1/responses/"+url.PathEscape(chi.URLParam(r, "id"))+"/cancel")
		})
		r.Get("/diagnostics", func(w http.ResponseWriter, r *http.Request) { d.proxy(w, r, "/v1/diagnostics?"+r.URL.RawQuery) })
	})
	for _, binding := range d.bindings {
		d.start(binding)
	}
}
func (d *agentxDevAdapter) request(ctx context.Context, method, path string, body io.Reader, key string) (*http.Response, error) {
	r, e := http.NewRequestWithContext(ctx, method, d.base+path, body)
	if e != nil {
		return nil, e
	}
	r.Header.Set("Authorization", "Bearer "+d.token)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	return (&http.Client{Timeout: 330 * time.Second}).Do(r)
}
func (d *agentxDevAdapter) proxy(w http.ResponseWriter, r *http.Request, path string) {
	var body io.Reader
	if r.Method != "GET" {
		body = http.MaxBytesReader(w, r.Body, 1<<20)
	}
	response, e := d.request(r.Context(), r.Method, path, body, r.Header.Get("Idempotency-Key"))
	if e != nil {
		writeCodedAPIError(w, 502, "agentx_unavailable", "AgentX is unavailable")
		return
	}
	defer response.Body.Close()
	for _, key := range []string{"Content-Type", "Cache-Control", "X-Response-Id"} {
		if v := response.Header.Get(key); v != "" {
			w.Header().Set(key, v)
		}
	}
	w.WriteHeader(response.StatusCode)
	buf := make([]byte, 8192)
	for {
		n, e := response.Body.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if e != nil {
			return
		}
	}
}
func (d *agentxDevAdapter) publish(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AgentID    string `json:"agent_id"`
		Name       string `json:"name"`
		Visibility string `json:"visibility"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		writeCodedAPIError(w, 400, "invalid_request", "Invalid publish request")
		return
	}
	selected, e := d.h.resolveSessionAgent(input.AgentID)
	if e != nil {
		writeCodedAPIError(w, 409, "agent_unavailable", e.Error())
		return
	}
	if input.Name == "" {
		input.Name = selected.Spec.Name
	}
	if input.Visibility == "" {
		input.Visibility = "public"
	}
	body, _ := json.Marshal(map[string]any{"name": input.Name, "description": selected.Spec.Description, "visibility": input.Visibility, "executor": "csgclaw", "source_agent_id": selected.ID})
	response, e := d.request(r.Context(), "POST", "/v1/agents", strings.NewReader(string(body)), r.Header.Get("Idempotency-Key"))
	if e != nil {
		writeCodedAPIError(w, 502, "agentx_unavailable", "AgentX is unavailable")
		return
	}
	defer response.Body.Close()
	data, e := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if e != nil {
		writeCodedAPIError(w, 502, "invalid_response", "Cannot read registration")
		return
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.StatusCode)
		_, _ = w.Write(data)
		return
	}
	var record struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	if json.Unmarshal(data, &record) != nil || record.Agent.ID == "" {
		writeCodedAPIError(w, 502, "invalid_response", "Invalid registration response")
		return
	}
	binding := agentxDevBinding{RemoteID: record.Agent.ID, SourceID: selected.ID}
	d.mu.Lock()
	d.bindings[binding.RemoteID] = binding
	e = d.saveBindingsLocked()
	d.mu.Unlock()
	if e != nil {
		writeCodedAPIError(w, 500, "binding_save_failed", "Cannot save local execution binding")
		return
	}
	d.start(binding)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(data)
}
func (d *agentxDevAdapter) saveBindingsLocked() error {
	if e := os.MkdirAll(filepath.Dir(d.statePath), 0700); e != nil {
		return e
	}
	data, e := json.Marshal(d.bindings)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(d.statePath), ".agentx-bindings-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(f.Name(), d.statePath)
}
func (d *agentxDevAdapter) start(binding agentxDevBinding) {
	d.mu.Lock()
	if _, ok := d.workers[binding.RemoteID]; ok {
		d.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(d.ctx)
	d.workers[binding.RemoteID] = cancel
	d.mu.Unlock()
	client := worker.New(worker.Options{BaseURL: d.base, Token: d.token, AgentID: binding.RemoteID,
		Health: func() (bool, string) {
			a, e := d.h.agentEngine.Agents().Get(ctx, binding.SourceID, agentengine.AgentGetOptions{ProbeRuntime: true})
			if e != nil {
				return false, "local_agent_unavailable"
			}
			return a.Status.State == agentengine.AgentStateRunning && a.Status.Ready, a.Status.Message
		},
		Execute: func(runCtx context.Context, session, input string, emit func(string) error) (string, error) {
			digest := sha256.Sum256([]byte(binding.RemoteID + "\x00" + session))
			sessionID := "agentx-" + hex.EncodeToString(digest[:])
			stored, e := d.h.sessionBindings.GetOrCreate(binding.SourceID, sessionID)
			if e != nil {
				return "", e
			}
			turnID := agentengine.TurnID(worker.ResponseID(runCtx))
			if turnID == "" {
				return "", errors.New("missing relay response identity")
			}
			slog.Info("agentx execution", "phase", "start", "response_id", turnID, "remote_agent_id", binding.RemoteID, "source_agent_id", binding.SourceID)
			result := d.h.agentEngine.Conversations(binding.SourceID).Run(runCtx, agentengine.TurnRequest{ID: turnID, ConversationKey: agentengine.ConversationKey(stored.ConversationKey), Admission: agentengine.AdmissionRejectIfBusy, Interaction: agentengine.InteractionReject, Input: []agentengine.InputPart{{Kind: agentengine.InputPartText, Text: input}}}, agentengine.EventSinkFunc(func(_ context.Context, event agentengine.TurnEvent) error {
				if event.Kind == agentengine.TurnEventTextDelta && event.Phase != "commentary" && !event.TextSnapshot {
					return emit(event.Text)
				}
				return nil
			}))
			slog.Info("agentx execution", "phase", "end", "response_id", turnID, "remote_agent_id", binding.RemoteID, "source_agent_id", binding.SourceID, "status", result.Status, "dispatched", result.Dispatched)
			if result.Status != agentengine.TurnSucceeded {
				if result.Error != nil {
					return "", errors.New(result.Error.Message)
				}
				return "", fmt.Errorf("agent execution: %s", result.Status)
			}
			return result.Output, nil
		},
	})
	go func() { _ = client.Run(ctx) }()
}
