package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/raym33/mi/internal/city"
	"github.com/raym33/mi/internal/config"
	"github.com/raym33/mi/internal/modelcatalog"
	"github.com/raym33/mi/internal/openai"
	"github.com/raym33/mi/internal/privacy"
	"github.com/raym33/mi/internal/protocol"
	"github.com/raym33/mi/internal/scheduler"
	"github.com/raym33/mi/internal/wsutil"
)

type server struct {
	registry              *scheduler.Registry
	market                *city.Market
	modelCatalog          *modelcatalog.Catalog
	adminToken            string
	devAdminOpen          bool
	requireNodeClientCert bool
}

const defaultReservedOutputTokens = 1024

func main() {
	configPath := flag.String("config", "configs/coordinator.yaml", "path to coordinator config")
	flag.Parse()

	cfg, err := config.LoadCoordinator(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	market, err := city.New(cfg.City, cfg.APIKeys)
	if err != nil {
		log.Fatal(err)
	}
	s := &server{
		registry:              scheduler.NewRegistry(),
		market:                market,
		modelCatalog:          modelcatalog.New(cfg.Models),
		adminToken:            cfg.AdminToken,
		devAdminOpen:          cfg.DevAdminOpen,
		requireNodeClientCert: cfg.TLS.NodeClientCAFile != "",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /network/status", s.networkStatus)
	mux.HandleFunc("GET /v1/me", s.requireConsumer(s.me))
	mux.HandleFunc("GET /v1/models", s.requireConsumer(s.models))
	mux.HandleFunc("GET /v1/models/catalog", s.requireConsumer(s.modelsCatalog))
	mux.HandleFunc("POST /v1/chat/completions", s.requireConsumerQuota(s.chatCompletions))
	mux.HandleFunc("GET /ws/node", s.nodeWebSocket)
	mux.HandleFunc("GET /admin/nodes", s.requireAdmin(s.adminNodes))
	mux.HandleFunc("GET /admin/city", s.requireAdmin(s.adminCity))
	mux.HandleFunc("POST /admin/consumers", s.requireAdmin(s.adminCreateConsumer))
	mux.HandleFunc("POST /admin/consumers/{id}/rotate-key", s.requireAdmin(s.adminRotateConsumerKey))
	mux.HandleFunc("DELETE /admin/consumers/{id}", s.requireAdmin(s.adminDisableConsumer))
	mux.HandleFunc("POST /admin/providers", s.requireAdmin(s.adminCreateProvider))
	mux.HandleFunc("POST /admin/providers/{id}/rotate-token", s.requireAdmin(s.adminRotateProviderToken))
	mux.HandleFunc("DELETE /admin/providers/{id}", s.requireAdmin(s.adminDisableProvider))

	log.Printf("mi coordinator listening on %s", cfg.ListenAddr)
	log.Fatal(serveHTTP(cfg, mux))
}

type contextKey string

const consumerIDKey contextKey = "consumer_id"

func (s *server) requireConsumer(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		consumerID, err := s.market.AuthenticateConsumer(bearerToken(r))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), consumerIDKey, consumerID))
		next(w, r)
	}
}

func (s *server) requireConsumerQuota(next http.HandlerFunc) http.HandlerFunc {
	return s.requireConsumer(func(w http.ResponseWriter, r *http.Request) {
		if err := s.market.CheckConsumerQuota(consumerID(r.Context())); err != nil {
			writeJSONStatus(w, http.StatusPaymentRequired, map[string]any{
				"error": map[string]string{
					"message": err.Error(),
					"type":    "quota_exceeded",
				},
			})
			return
		}
		next(w, r)
	})
}

func (s *server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.adminToken == "" && s.devAdminOpen {
			next(w, r)
			return
		}
		if s.adminToken == "" {
			http.Error(w, "admin token required", http.StatusUnauthorized)
			return
		}
		if bearerToken(r) != s.adminToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *server) networkStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.registry.NetworkStatus())
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.market.ConsumerStatus(consumerID(r.Context())))
}

func (s *server) models(w http.ResponseWriter, _ *http.Request) {
	now := time.Now().Unix()
	models := s.modelCatalog.VisibleModelIDs(s.registry.Models())
	data := make([]openai.Model, 0, len(models))
	for _, model := range models {
		data = append(data, openai.Model{ID: model, Object: "model", Created: now, OwnedBy: "mi"})
	}
	writeJSON(w, openai.ModelList{Object: "list", Data: data})
}

func (s *server) modelsCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.modelCatalog.Catalog(s.registry.Models()))
}

func (s *server) adminNodes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.registry.Snapshot())
}

func (s *server) adminCity(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.market.Snapshot())
}

func (s *server) adminCreateConsumer(w http.ResponseWriter, r *http.Request) {
	var req city.CreateConsumerInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	created, err := s.market.CreateConsumer(req)
	if err != nil {
		writeCreateError(w, err)
		return
	}
	writeJSONStatus(w, http.StatusCreated, created)
}

func (s *server) adminCreateProvider(w http.ResponseWriter, r *http.Request) {
	var req city.CreateProviderInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	created, err := s.market.CreateProvider(req)
	if err != nil {
		writeCreateError(w, err)
		return
	}
	writeJSONStatus(w, http.StatusCreated, created)
}

func (s *server) adminRotateConsumerKey(w http.ResponseWriter, r *http.Request) {
	rotated, err := s.market.RotateConsumerKey(r.PathValue("id"))
	if err != nil {
		writeAccountError(w, err)
		return
	}
	writeJSON(w, rotated)
}

func (s *server) adminRotateProviderToken(w http.ResponseWriter, r *http.Request) {
	rotated, err := s.market.RotateProviderToken(r.PathValue("id"))
	if err != nil {
		writeAccountError(w, err)
		return
	}
	writeJSON(w, rotated)
}

func (s *server) adminDisableConsumer(w http.ResponseWriter, r *http.Request) {
	consumer, err := s.market.DisableConsumer(r.PathValue("id"))
	if err != nil {
		writeAccountError(w, err)
		return
	}
	writeJSON(w, map[string]any{"consumer": consumer})
}

func (s *server) adminDisableProvider(w http.ResponseWriter, r *http.Request) {
	provider, err := s.market.DisableProvider(r.PathValue("id"))
	if err != nil {
		writeAccountError(w, err)
		return
	}
	disconnected := s.registry.RemoveProvider(provider.ID)
	writeJSON(w, map[string]any{"provider": provider, "disconnected_nodes": disconnected})
}

func (s *server) chatCompletions(w http.ResponseWriter, r *http.Request) {
	var req openai.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Model == "" || len(req.Messages) == 0 {
		http.Error(w, "model and messages are required", http.StatusBadRequest)
		return
	}

	requestID := "chatcmpl-" + randomID()
	privacyTier, err := requestPrivacyTier(r, req)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"message": "privacy_tier must be one of private, community, public",
				"type":    "invalid_privacy_tier",
			},
		})
		return
	}
	modelResolution := s.modelCatalog.Resolve(req.Model)
	inferReq := protocol.InferRequest{
		Model:       modelResolution.Target,
		Stream:      req.Stream,
		PrivacyTier: privacyTier,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	for _, msg := range req.Messages {
		inferReq.Messages = append(inferReq.Messages, protocol.ProtocolMessage{Role: msg.Role, Content: msg.Content})
	}
	reservation, err := s.market.ReserveConsumerQuota(consumerID(r.Context()), estimateTokenBudget(req))
	if err != nil {
		writeJSONStatus(w, http.StatusPaymentRequired, map[string]any{
			"error": map[string]string{
				"message": err.Error(),
				"type":    "quota_exceeded",
			},
		})
		return
	}
	if reservation != nil && inferReq.MaxTokens == nil {
		defaultMaxTokens := defaultReservedOutputTokens
		inferReq.MaxTokens = &defaultMaxTokens
	}

	if req.Stream {
		s.streamChat(w, r, requestID, req.Model, inferReq, consumerID(r.Context()), reservation)
		return
	}
	s.blockingChat(w, r, requestID, req.Model, inferReq, consumerID(r.Context()), reservation)
}

func (s *server) streamChat(w http.ResponseWriter, r *http.Request, requestID string, responseModel string, req protocol.InferRequest, consumerID string, reservation *city.QuotaReservation) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.market.ReleaseReservation(reservation)
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Mi-Privacy-Tier", req.PrivacyTier)
	declareDispatchTrailers(w)

	sink := sseSink{w: w, flusher: flusher, requestID: requestID, model: responseModel}
	result, err := s.registry.Dispatch(r.Context(), requestID, req, &sink)
	setDispatchTrailers(w, result)
	if err != nil {
		s.market.ReleaseReservation(reservation)
		s.writeStreamError(w, flusher, err)
		return
	}
	done := result.Done
	if err := s.market.RecordReserved(reservation, consumerID, result.ProviderID, done); err != nil {
		log.Printf("record usage: %v", err)
	}
	finish := done.FinishReason
	chunk := openai.ChatCompletionChunk{
		ID:      requestID,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   responseModel,
		Choices: []openai.ChunkChoice{{Index: 0, Delta: openai.ChatMessage{}, FinishReason: &finish}},
	}
	writeSSE(w, flusher, chunk)
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (s *server) blockingChat(w http.ResponseWriter, r *http.Request, requestID string, responseModel string, req protocol.InferRequest, consumerID string, reservation *city.QuotaReservation) {
	var content string
	sink := collectSink{onChunk: func(chunk string) { content += chunk }}
	w.Header().Set("X-Mi-Privacy-Tier", req.PrivacyTier)
	result, err := s.registry.Dispatch(r.Context(), requestID, req, sink)
	setDispatchHeaders(w, result)
	if err != nil {
		s.market.ReleaseReservation(reservation)
		status := http.StatusBadGateway
		if errors.Is(err, scheduler.ErrNoNode) {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return
	}
	done := result.Done
	if err := s.market.RecordReserved(reservation, consumerID, result.ProviderID, done); err != nil {
		log.Printf("record usage: %v", err)
	}
	writeJSON(w, openai.ChatCompletionResponse{
		ID:      requestID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   responseModel,
		Choices: []openai.ResponseChoice{{
			Index:        0,
			Message:      openai.ChatMessage{Role: "assistant", Content: content},
			FinishReason: done.FinishReason,
		}},
		Usage: openai.Usage{
			PromptTokens:     done.PromptTokens,
			CompletionTokens: done.OutputTokens,
			TotalTokens:      done.PromptTokens + done.OutputTokens,
		},
	})
}

func (s *server) writeStreamError(w http.ResponseWriter, flusher http.Flusher, err error) {
	payload := map[string]any{"error": map[string]string{"message": err.Error(), "type": "mi_error"}}
	data, _ := json.Marshal(payload)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func (s *server) nodeWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.requireNodeClientCert && !hasClientCertificate(r) {
		http.Error(w, "node client certificate required", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("accept node websocket: %v", err)
		return
	}
	nc := newNodeConn(conn)
	defer nc.Close()

	ctx := r.Context()
	first, err := wsutil.ReadJSON[protocol.Envelope](ctx, conn)
	if err != nil || first.Type != "register" || first.Register == nil || first.Register.NodeID == "" {
		_ = conn.Close(websocket.StatusPolicyViolation, "register required")
		return
	}
	providerID, err := s.market.AuthenticateProvider(*first.Register)
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "provider token required")
		return
	}
	first.Register.ProviderID = providerID
	privacyMode, privacyTiers, err := s.market.EnforceProviderPrivacy(providerID, first.Register.PrivacyMode, first.Register.PrivacyTiers)
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "provider privacy policy rejected")
		return
	}
	first.Register.PrivacyMode = privacyMode
	first.Register.PrivacyTiers = privacyTiers
	nodeID := first.Register.NodeID
	s.registry.Register(*first.Register, nc)
	defer s.registry.Remove(nodeID)
	log.Printf("node registered: %s (%s)", nodeID, first.Register.Hostname)

	for {
		msg, err := wsutil.ReadJSON[protocol.Envelope](ctx, conn)
		if err != nil {
			log.Printf("node disconnected: %s: %v", nodeID, err)
			return
		}
		switch msg.Type {
		case "heartbeat":
			if msg.Heartbeat != nil {
				s.registry.Heartbeat(*msg.Heartbeat)
			}
		case "chunk", "done", "error":
			nc.deliver(msg)
		}
	}
}

type nodeConn struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan protocol.Envelope
}

func newNodeConn(conn *websocket.Conn) *nodeConn {
	return &nodeConn{conn: conn, pending: map[string]chan protocol.Envelope{}}
}

func (n *nodeConn) SendInference(ctx context.Context, requestID string, req protocol.InferRequest, sink scheduler.StreamSink) (protocol.InferDone, error) {
	ch := make(chan protocol.Envelope, 128)
	n.mu.Lock()
	n.pending[requestID] = ch
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		delete(n.pending, requestID)
		n.mu.Unlock()
	}()

	if err := n.write(ctx, protocol.Envelope{Type: "infer", RequestID: requestID, Infer: &req}); err != nil {
		return protocol.InferDone{}, err
	}
	for {
		select {
		case <-ctx.Done():
			return protocol.InferDone{}, ctx.Err()
		case msg := <-ch:
			switch msg.Type {
			case "chunk":
				if msg.Chunk != nil {
					if err := sink.Chunk(msg.Chunk.Content); err != nil {
						return protocol.InferDone{}, err
					}
				}
			case "done":
				if msg.Done == nil {
					return protocol.InferDone{FinishReason: "stop"}, nil
				}
				return *msg.Done, nil
			case "error":
				if msg.Error != nil {
					return protocol.InferDone{}, nodeInferenceError{message: msg.Error.Message, retryable: msg.Error.Retryable}
				}
				return protocol.InferDone{}, errors.New("node returned inference error")
			}
		}
	}
}

func (n *nodeConn) deliver(msg protocol.Envelope) {
	n.mu.Lock()
	ch := n.pending[msg.RequestID]
	n.mu.Unlock()
	if ch == nil {
		return
	}
	ch <- msg
}

func (n *nodeConn) write(ctx context.Context, msg protocol.Envelope) error {
	n.writeMu.Lock()
	defer n.writeMu.Unlock()
	return wsutil.WriteJSON(ctx, n.conn, msg)
}

func (n *nodeConn) Close() error {
	n.mu.Lock()
	for requestID, ch := range n.pending {
		ch <- protocol.Envelope{
			Type:      "error",
			RequestID: requestID,
			Error:     &protocol.InferError{Message: "node disconnected", Retryable: true},
		}
	}
	n.pending = map[string]chan protocol.Envelope{}
	n.mu.Unlock()
	return n.conn.Close(websocket.StatusNormalClosure, "")
}

type sseSink struct {
	w         http.ResponseWriter
	flusher   http.Flusher
	requestID string
	model     string
	sentRole  bool
}

func (s *sseSink) Chunk(content string) error {
	role := ""
	if !s.sentRole {
		role = "assistant"
		s.sentRole = true
	}
	chunk := openai.ChatCompletionChunk{
		ID:      s.requestID,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   s.model,
		Choices: []openai.ChunkChoice{{Index: 0, Delta: openai.ChatMessage{Role: role, Content: content}}},
	}
	writeSSE(s.w, s.flusher, chunk)
	return nil
}

type collectSink struct {
	onChunk func(string)
}

func (s collectSink) Chunk(content string) error {
	s.onChunk(content)
	return nil
}

type nodeInferenceError struct {
	message   string
	retryable bool
}

func (e nodeInferenceError) Error() string {
	return e.message
}

func (e nodeInferenceError) Retryable() bool {
	return e.retryable
}

func writeJSON(w http.ResponseWriter, value any) {
	writeJSONStatus(w, http.StatusOK, value)
}

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, value any) {
	data, _ := json.Marshal(value)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func setDispatchHeaders(w http.ResponseWriter, result scheduler.DispatchResult) {
	if result.Attempts > 0 {
		w.Header().Set("X-Mi-Dispatch-Attempts", strconv.Itoa(result.Attempts))
	}
	if result.NodeID != "" {
		w.Header().Set("X-Mi-Node-Id", result.NodeID)
	}
	if result.ProviderID != "" {
		w.Header().Set("X-Mi-Provider-Id", result.ProviderID)
	}
}

func requestPrivacyTier(r *http.Request, req openai.ChatCompletionRequest) (string, error) {
	if tier := r.Header.Get("X-Mi-Privacy-Tier"); tier != "" {
		return privacy.NormalizeTier(tier)
	}
	return privacy.NormalizeTier(req.PrivacyTier)
}

func estimateTokenBudget(req openai.ChatCompletionRequest) int64 {
	chars := 0
	for _, msg := range req.Messages {
		chars += len(msg.Role) + len(msg.Content)
	}
	promptEstimate := int64((chars + 3) / 4)
	outputEstimate := int64(defaultReservedOutputTokens)
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		outputEstimate = int64(*req.MaxTokens)
	}
	return promptEstimate + outputEstimate
}

func declareDispatchTrailers(w http.ResponseWriter) {
	w.Header().Add("Trailer", "X-Mi-Dispatch-Attempts")
	w.Header().Add("Trailer", "X-Mi-Node-Id")
	w.Header().Add("Trailer", "X-Mi-Provider-Id")
}

func setDispatchTrailers(w http.ResponseWriter, result scheduler.DispatchResult) {
	setDispatchHeaders(w, result)
}

func writeCreateError(w http.ResponseWriter, err error) {
	writeAccountError(w, err)
}

func writeAccountError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	errorType := "internal_error"
	switch {
	case errors.Is(err, city.ErrInvalidAccount):
		status = http.StatusBadRequest
		errorType = "invalid_account"
	case errors.Is(err, city.ErrAccountExists):
		status = http.StatusConflict
		errorType = "account_exists"
	case errors.Is(err, city.ErrAccountNotFound):
		status = http.StatusNotFound
		errorType = "account_not_found"
	case errors.Is(err, city.ErrAccountDisabled):
		status = http.StatusConflict
		errorType = "account_disabled"
	case errors.Is(err, city.ErrInvalidPrivacy):
		status = http.StatusBadRequest
		errorType = "invalid_privacy"
	}
	writeJSONStatus(w, status, map[string]any{
		"error": map[string]string{
			"message": err.Error(),
			"type":    errorType,
		},
	})
}

func randomID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func bearerToken(r *http.Request) string {
	token := r.Header.Get("Authorization")
	if len(token) > 7 && token[:7] == "Bearer " {
		return token[7:]
	}
	if token != "" {
		return token
	}
	return r.Header.Get("X-API-Key")
}

func consumerID(ctx context.Context) string {
	if value, ok := ctx.Value(consumerIDKey).(string); ok && value != "" {
		return value
	}
	return "local"
}

func serveHTTP(cfg config.Coordinator, handler http.Handler) error {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.TLS.NodeClientCAFile != "" {
		certPool, err := loadCertPool(cfg.TLS.NodeClientCAFile)
		if err != nil {
			return err
		}
		tlsConfig.ClientCAs = certPool
		tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
	}
	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: handler,
		// Keep WebSocket upgrades on HTTP/1.1 until the provider wire protocol
		// explicitly supports RFC 8441 WebSockets over HTTP/2.
		TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){},
		TLSConfig:    tlsConfig,
	}
	if cfg.TLS.CertFile != "" || cfg.TLS.KeyFile != "" {
		if cfg.TLS.CertFile == "" || cfg.TLS.KeyFile == "" {
			return errors.New("both tls.cert_file and tls.key_file are required")
		}
		log.Printf("TLS enabled for coordinator")
		return server.ListenAndServeTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile)
	}
	if cfg.TLS.NodeClientCAFile != "" {
		return errors.New("tls.node_client_ca_file requires tls.cert_file and tls.key_file")
	}
	return server.ListenAndServe()
}

func loadCertPool(path string) (*x509.CertPool, error) {
	certPEM, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		return nil, errors.New("failed to parse certificate authority file")
	}
	return pool, nil
}

func hasClientCertificate(r *http.Request) bool {
	return r.TLS != nil && len(r.TLS.PeerCertificates) > 0
}
