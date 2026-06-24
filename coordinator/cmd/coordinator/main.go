package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	"github.com/raym33/mi/internal/challenge"
	"github.com/raym33/mi/internal/city"
	"github.com/raym33/mi/internal/config"
	"github.com/raym33/mi/internal/idempotency"
	"github.com/raym33/mi/internal/modelcatalog"
	"github.com/raym33/mi/internal/openai"
	"github.com/raym33/mi/internal/privacy"
	"github.com/raym33/mi/internal/protocol"
	"github.com/raym33/mi/internal/reputation"
	"github.com/raym33/mi/internal/scheduler"
	"github.com/raym33/mi/internal/settlement"
	"github.com/raym33/mi/internal/wsutil"
)

type server struct {
	registry              nodeRegistry
	market                accountMarket
	modelCatalog          catalogService
	settlement            settlementLedger
	idempotency           *idempotency.Store
	challenges            challengeLedger
	challengeMu           sync.Mutex
	nextChallengeProvider int
	adminToken            string
	devAdminOpen          bool
	requireNodeClientCert bool
	requestTimeout        time.Duration
	maxRequestBytes       int64
}

const defaultMaxRequestBytes = 8 << 20 // 8 MiB

const defaultReservedOutputTokens = 1024
const defaultReputationRefreshInterval = 30 * time.Second
const shutdownTimeout = 20 * time.Second

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
	settlementLedger, err := settlement.New(cfg.Settlement)
	if err != nil {
		_ = market.Close()
		log.Fatal(err)
	}
	challengeLedger, err := challenge.New(cfg.Challenges)
	if err != nil {
		_ = settlementLedger.Close()
		_ = market.Close()
		log.Fatal(err)
	}
	idemStore, err := idempotency.New(cfg.Idempotency.SQLitePath, cfg.Idempotency.TTL.Duration)
	if err != nil {
		_ = settlementLedger.Close()
		_ = market.Close()
		log.Fatal(err)
	}
	closeState := func() {
		_ = idemStore.Close()
		_ = settlementLedger.Close()
		_ = market.Close()
	}
	s := &server{
		registry:              scheduler.NewRegistry(),
		market:                market,
		modelCatalog:          modelcatalog.New(cfg.Models),
		settlement:            settlementLedger,
		idempotency:           idemStore,
		challenges:            challengeLedger,
		adminToken:            cfg.AdminToken,
		devAdminOpen:          cfg.DevAdminOpen,
		requireNodeClientCert: cfg.TLS.NodeClientCAFile != "",
		requestTimeout:        cfg.Scheduler.RequestTimeout.Duration,
		maxRequestBytes:       maxRequestBytesOrDefault(cfg.MaxRequestBytes),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /network/status", s.networkStatus)
	mux.HandleFunc("GET /v1/me", s.requireConsumer(s.me))
	mux.HandleFunc("GET /v1/models", s.requireConsumer(s.models))
	mux.HandleFunc("GET /v1/models/catalog", s.requireConsumer(s.modelsCatalog))
	mux.HandleFunc("POST /v1/chat/completions", s.requireConsumerQuota(s.chatCompletions))
	mux.HandleFunc("POST /v1/embeddings", s.requireConsumerQuota(s.embeddings))
	mux.HandleFunc("GET /ws/node", s.nodeWebSocket)
	mux.HandleFunc("GET /admin", s.adminDashboardRedirect)
	mux.HandleFunc("GET /admin/dashboard", s.adminDashboard)
	mux.HandleFunc("GET /admin/metrics", s.requireAdmin(s.adminMetrics))
	mux.HandleFunc("GET /admin/nodes", s.requireAdmin(s.adminNodes))
	mux.HandleFunc("GET /admin/city", s.requireAdmin(s.adminCity))
	mux.HandleFunc("GET /admin/settlement", s.requireAdmin(s.adminSettlement))
	mux.HandleFunc("GET /admin/settlement/verify", s.requireAdmin(s.adminSettlementVerify))
	mux.HandleFunc("GET /admin/payouts.csv", s.requireAdmin(s.adminPayoutsCSV))
	mux.HandleFunc("GET /admin/reputation", s.requireAdmin(s.adminReputation))
	mux.HandleFunc("GET /admin/integrity", s.requireAdmin(s.adminIntegrity))
	mux.HandleFunc("GET /admin/challenges", s.requireAdmin(s.adminChallenges))
	mux.HandleFunc("POST /admin/challenges", s.requireAdmin(s.adminRecordChallenge))
	mux.HandleFunc("POST /admin/challenges/run", s.requireAdmin(s.adminRunChallenge))
	mux.HandleFunc("GET /admin/challenges/verify", s.requireAdmin(s.adminChallengesVerify))
	mux.HandleFunc("POST /admin/consumers", s.requireAdmin(s.adminCreateConsumer))
	mux.HandleFunc("POST /admin/consumers/{id}/rotate-key", s.requireAdmin(s.adminRotateConsumerKey))
	mux.HandleFunc("DELETE /admin/consumers/{id}", s.requireAdmin(s.adminDisableConsumer))
	mux.HandleFunc("POST /admin/providers", s.requireAdmin(s.adminCreateProvider))
	mux.HandleFunc("POST /admin/providers/{id}/rotate-token", s.requireAdmin(s.adminRotateProviderToken))
	mux.HandleFunc("DELETE /admin/providers/{id}", s.requireAdmin(s.adminDisableProvider))

	srv, err := newHTTPServer(cfg, mux)
	if err != nil {
		closeState()
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s.startReputationRefresher(ctx, defaultReputationRefreshInterval)
	s.startChallengeRunner(ctx, cfg.Challenges)

	log.Printf("mi coordinator listening on %s", cfg.ListenAddr)
	serveErr := make(chan error, 1)
	go func() { serveErr <- listenAndServe(srv, cfg) }()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			closeState()
			log.Fatal(err)
		}
	case <-ctx.Done():
		// Restore default signal handling so a second signal force-kills a
		// process that is stuck draining.
		stop()
		log.Printf("shutdown signal received; draining in-flight requests (timeout %s)", shutdownTimeout)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful drain incomplete (%v); forcing connections closed", err)
			_ = srv.Close()
		}
		closeState()
		log.Printf("coordinator shut down cleanly")
	}
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

func (s *server) adminMetrics(w http.ResponseWriter, _ *http.Request) {
	var status scheduler.NetworkStatus
	var nodes []scheduler.NodeView
	if s.registry != nil {
		status = s.registry.NetworkStatus()
		nodes = s.registry.Snapshot()
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	var settlementSnapshot settlement.Snapshot
	if s.settlement != nil {
		settlementSnapshot = s.settlement.Snapshot(0)
	}
	providerBalances := append([]settlement.Balance(nil), settlementSnapshot.ProviderBalances...)
	sort.Slice(providerBalances, func(i, j int) bool { return providerBalances[i].AccountID < providerBalances[j].AccountID })

	var challengeSnapshot challenge.Snapshot
	if s.challenges != nil {
		challengeSnapshot = s.challenges.Snapshot(0)
	}
	providerChallenges := append([]challenge.ProviderSummary(nil), challengeSnapshot.Summaries...)
	sort.Slice(providerChallenges, func(i, j int) bool { return providerChallenges[i].ProviderID < providerChallenges[j].ProviderID })

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	writePrometheusHeader(w, "mi_nodes", "Registered nodes.", "gauge")
	writePrometheusSample(w, "mi_nodes", nil, prometheusInt(int64(status.Nodes)))
	writePrometheusHeader(w, "mi_healthy_nodes", "Healthy nodes available for routing.", "gauge")
	writePrometheusSample(w, "mi_healthy_nodes", nil, prometheusInt(int64(status.HealthyNodes)))
	writePrometheusHeader(w, "mi_cooldown_nodes", "Nodes currently in cooldown.", "gauge")
	writePrometheusSample(w, "mi_cooldown_nodes", nil, prometheusInt(int64(status.CooldownNodes)))
	writePrometheusHeader(w, "mi_active_requests", "Active inference requests across healthy nodes.", "gauge")
	writePrometheusSample(w, "mi_active_requests", nil, prometheusInt(int64(status.ActiveRequests)))
	writePrometheusHeader(w, "mi_available_slots", "Available request slots across healthy nodes.", "gauge")
	writePrometheusSample(w, "mi_available_slots", nil, prometheusInt(int64(status.AvailableSlots)))
	writePrometheusHeader(w, "mi_max_concurrent", "Maximum concurrent request capacity across healthy nodes.", "gauge")
	writePrometheusSample(w, "mi_max_concurrent", nil, prometheusInt(int64(status.MaxConcurrent)))
	writePrometheusHeader(w, "mi_total_memory_free_mb", "Total free memory advertised by healthy nodes in megabytes.", "gauge")
	writePrometheusSample(w, "mi_total_memory_free_mb", nil, prometheusUint(status.TotalMemoryFreeMB))
	writePrometheusHeader(w, "mi_average_latency_ms", "Average observed latency across healthy nodes in milliseconds.", "gauge")
	writePrometheusSample(w, "mi_average_latency_ms", nil, prometheusInt(status.AverageLatencyMs))
	writePrometheusHeader(w, "mi_average_ttft_ms", "Average observed time to first token across healthy nodes in milliseconds.", "gauge")
	writePrometheusSample(w, "mi_average_ttft_ms", nil, prometheusInt(status.AverageTTFTMs))
	writePrometheusHeader(w, "mi_average_tokens_per_second", "Average observed output tokens per second across healthy nodes.", "gauge")
	writePrometheusSample(w, "mi_average_tokens_per_second", nil, prometheusFloat(status.AverageTokensPerSecond))

	writePrometheusHeader(w, "mi_node_healthy", "Node health state, 1 for healthy and 0 for unhealthy.", "gauge")
	for _, node := range nodes {
		writePrometheusSample(w, "mi_node_healthy", nodePrometheusLabels(node), prometheusBool(node.Healthy))
	}
	writePrometheusHeader(w, "mi_node_active", "Active requests on the node.", "gauge")
	for _, node := range nodes {
		writePrometheusSample(w, "mi_node_active", nodePrometheusLabels(node), prometheusInt(int64(node.Active)))
	}
	writePrometheusHeader(w, "mi_node_max_concurrent", "Maximum concurrent requests accepted by the node.", "gauge")
	for _, node := range nodes {
		writePrometheusSample(w, "mi_node_max_concurrent", nodePrometheusLabels(node), prometheusInt(int64(node.MaxConcurrent)))
	}
	writePrometheusHeader(w, "mi_node_completed_requests_total", "Completed requests observed for the node.", "counter")
	for _, node := range nodes {
		writePrometheusSample(w, "mi_node_completed_requests_total", nodePrometheusLabels(node), prometheusInt(node.CompletedRequests))
	}
	writePrometheusHeader(w, "mi_node_failed_requests_total", "Failed requests observed for the node.", "counter")
	for _, node := range nodes {
		writePrometheusSample(w, "mi_node_failed_requests_total", nodePrometheusLabels(node), prometheusInt(node.FailedRequests))
	}
	writePrometheusHeader(w, "mi_node_observed_latency_ms", "Node observed latency in milliseconds.", "gauge")
	for _, node := range nodes {
		writePrometheusSample(w, "mi_node_observed_latency_ms", nodePrometheusLabels(node), prometheusInt(node.ObservedLatencyMs))
	}
	writePrometheusHeader(w, "mi_node_observed_ttft_ms", "Node observed time to first token in milliseconds.", "gauge")
	for _, node := range nodes {
		writePrometheusSample(w, "mi_node_observed_ttft_ms", nodePrometheusLabels(node), prometheusInt(node.ObservedTTFTMs))
	}
	writePrometheusHeader(w, "mi_node_observed_tokens_per_second", "Node observed output tokens per second.", "gauge")
	for _, node := range nodes {
		writePrometheusSample(w, "mi_node_observed_tokens_per_second", nodePrometheusLabels(node), prometheusFloat(node.ObservedTokensPerSecond))
	}
	writePrometheusHeader(w, "mi_node_provider_score", "Provider reputation score applied to the node.", "gauge")
	for _, node := range nodes {
		writePrometheusSample(w, "mi_node_provider_score", nodePrometheusLabels(node), prometheusInt(int64(node.ProviderScore)))
	}

	writePrometheusHeader(w, "mi_settlement_events_total", "Settlement events recorded by the coordinator.", "counter")
	writePrometheusSample(w, "mi_settlement_events_total", nil, prometheusInt(int64(settlementSnapshot.Events)))
	writePrometheusHeader(w, "mi_provider_reward_micros", "Provider reward balance in micros.", "gauge")
	for _, balance := range providerBalances {
		writePrometheusSample(w, "mi_provider_reward_micros", providerPrometheusLabels(balance.AccountID), prometheusInt(balance.RewardMicros))
	}
	writePrometheusHeader(w, "mi_provider_penalty_micros", "Provider penalty balance in micros.", "gauge")
	for _, balance := range providerBalances {
		writePrometheusSample(w, "mi_provider_penalty_micros", providerPrometheusLabels(balance.AccountID), prometheusInt(balance.PenaltyMicros))
	}
	writePrometheusHeader(w, "mi_provider_total_tokens", "Provider token balance from settlement accounting.", "gauge")
	for _, balance := range providerBalances {
		writePrometheusSample(w, "mi_provider_total_tokens", providerPrometheusLabels(balance.AccountID), prometheusInt(balance.TotalTokens))
	}
	writePrometheusHeader(w, "mi_provider_events_total", "Settlement events recorded for the provider.", "counter")
	for _, balance := range providerBalances {
		writePrometheusSample(w, "mi_provider_events_total", providerPrometheusLabels(balance.AccountID), prometheusInt(balance.Events))
	}

	writePrometheusHeader(w, "mi_provider_challenges_total", "Challenges recorded for the provider.", "counter")
	for _, summary := range providerChallenges {
		writePrometheusSample(w, "mi_provider_challenges_total", providerPrometheusLabels(summary.ProviderID), prometheusInt(summary.Challenges))
	}
	writePrometheusHeader(w, "mi_provider_challenges_passed_total", "Challenges passed by the provider.", "counter")
	for _, summary := range providerChallenges {
		writePrometheusSample(w, "mi_provider_challenges_passed_total", providerPrometheusLabels(summary.ProviderID), prometheusInt(summary.Passed))
	}
	writePrometheusHeader(w, "mi_provider_challenges_failed_total", "Challenges failed by the provider.", "counter")
	for _, summary := range providerChallenges {
		writePrometheusSample(w, "mi_provider_challenges_failed_total", providerPrometheusLabels(summary.ProviderID), prometheusInt(summary.Failed))
	}
	writePrometheusHeader(w, "mi_provider_challenge_pass_rate_bps", "Provider challenge pass rate in basis points.", "gauge")
	for _, summary := range providerChallenges {
		writePrometheusSample(w, "mi_provider_challenge_pass_rate_bps", providerPrometheusLabels(summary.ProviderID), prometheusInt(summary.PassRateBPS))
	}
}

type prometheusLabel struct {
	Name      string
	Value     string
	OmitEmpty bool
}

func writePrometheusHeader(w io.Writer, name string, help string, metricType string) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s %s\n", name, metricType)
}

func writePrometheusSample(w io.Writer, name string, labels []prometheusLabel, value string) {
	fmt.Fprintf(w, "%s%s %s\n", name, prometheusLabelSet(labels), value)
}

func prometheusLabelSet(labels []prometheusLabel) string {
	var b strings.Builder
	for _, label := range labels {
		if label.OmitEmpty && label.Value == "" {
			continue
		}
		if b.Len() == 0 {
			b.WriteByte('{')
		} else {
			b.WriteByte(',')
		}
		b.WriteString(label.Name)
		b.WriteString("=\"")
		b.WriteString(escapePrometheusLabel(label.Value))
		b.WriteByte('"')
	}
	if b.Len() == 0 {
		return ""
	}
	b.WriteByte('}')
	return b.String()
}

var prometheusLabelEscaper = strings.NewReplacer(
	"\\", "\\\\",
	"\"", "\\\"",
	"\n", "\\n",
)

func escapePrometheusLabel(value string) string {
	return prometheusLabelEscaper.Replace(value)
}

func nodePrometheusLabels(node scheduler.NodeView) []prometheusLabel {
	// privacy_tier is omitted because snapshots have no clean per-tier aggregate today.
	return []prometheusLabel{
		{Name: "node_id", Value: node.ID},
		{Name: "provider_id", Value: node.ProviderID},
		{Name: "backend", Value: node.Backend, OmitEmpty: true},
		{Name: "device_kind", Value: node.DeviceKind, OmitEmpty: true},
	}
}

func providerPrometheusLabels(providerID string) []prometheusLabel {
	return []prometheusLabel{{Name: "provider_id", Value: providerID}}
}

func prometheusBool(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func prometheusInt(value int64) string {
	return strconv.FormatInt(value, 10)
}

func prometheusUint(value uint64) string {
	return strconv.FormatUint(value, 10)
}

func prometheusFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func (s *server) adminCity(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.market.Snapshot())
}

func (s *server) adminSettlement(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	writeJSON(w, s.settlement.Snapshot(limit))
}

func (s *server) adminSettlementVerify(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.settlement.Verify())
}

func (s *server) adminPayoutsCSV(w http.ResponseWriter, _ *http.Request) {
	displayNames := map[string]string{}
	for _, provider := range s.market.Snapshot().Providers {
		displayNames[provider.ID] = provider.DisplayName
	}
	settlementSnapshot := s.settlement.Snapshot(0)
	balances := append([]settlement.Balance(nil), settlementSnapshot.ProviderBalances...)
	sort.Slice(balances, func(i, j int) bool { return balances[i].AccountID < balances[j].AccountID })

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="mi-payouts.csv"`)
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"provider_id", "display_name", "events", "total_tokens", "avg_latency_ms", "reward_micros", "penalty_micros"}); err != nil {
		return
	}
	for _, balance := range balances {
		if err := writer.Write([]string{
			balance.AccountID,
			displayNames[balance.AccountID],
			strconv.FormatInt(balance.Events, 10),
			strconv.FormatInt(balance.TotalTokens, 10),
			strconv.FormatInt(balance.AverageLatencyMs, 10),
			strconv.FormatInt(balance.RewardMicros, 10),
			strconv.FormatInt(balance.PenaltyMicros, 10),
		}); err != nil {
			return
		}
	}
	writer.Flush()
}

func (s *server) adminReputation(w http.ResponseWriter, _ *http.Request) {
	report := s.reputationReport()
	s.applyProviderScores(report)
	writeJSON(w, report)
}

func (s *server) refreshSchedulerReputation() {
	if s.registry == nil || s.market == nil {
		return
	}
	s.applyProviderScores(s.reputationReport())
}

func (s *server) startReputationRefresher(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultReputationRefreshInterval
	}
	s.refreshSchedulerReputation()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.refreshSchedulerReputation()
			}
		}
	}()
}

func (s *server) reputationReport() reputation.Report {
	var citySnapshot city.Snapshot
	if s.market != nil {
		citySnapshot = s.market.Snapshot()
	}
	var settlementSnapshot settlement.Snapshot
	if s.settlement != nil {
		settlementSnapshot = s.settlement.Snapshot(0)
	}
	var challengeSnapshot challenge.Snapshot
	if s.challenges != nil {
		challengeSnapshot = s.challenges.Snapshot(0)
	}
	var nodes []scheduler.NodeView
	if s.registry != nil {
		nodes = s.registry.Snapshot()
	}
	return reputation.Build(citySnapshot, nodes, settlementSnapshot, challengeSnapshot)
}

func (s *server) applyProviderScores(report reputation.Report) {
	if s.registry == nil {
		return
	}
	scores := map[string]int{}
	for _, provider := range report.Providers {
		scores[provider.ProviderID] = provider.Score
	}
	s.registry.SetProviderScores(scores)
}

func (s *server) adminIntegrity(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.integrityReport())
}

func (s *server) adminChallenges(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	writeJSON(w, s.challenges.Snapshot(limit))
}

func (s *server) adminChallengesVerify(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.challenges.Verify())
}

func (s *server) adminRecordChallenge(w http.ResponseWriter, r *http.Request) {
	var req challenge.RecordInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ProviderID == "" || req.Challenge == "" {
		http.Error(w, "provider_id and challenge are required", http.StatusBadRequest)
		return
	}
	event, err := s.challenges.Record(req)
	if err != nil {
		if errors.Is(err, challenge.ErrDisabled) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, http.StatusCreated, event)
}

type integrityReport struct {
	GeneratedAt time.Time               `json:"generated_at"`
	Valid       bool                    `json:"valid"`
	Settlement  settlement.Verification `json:"settlement"`
	Challenges  challenge.Verification  `json:"challenges"`
	Anchor      integrityAnchor         `json:"anchor"`
}

type integrityAnchor struct {
	Version          string    `json:"version"`
	GeneratedAt      time.Time `json:"generated_at"`
	SettlementEvents int       `json:"settlement_events"`
	SettlementHash   string    `json:"settlement_hash"`
	ChallengeEvents  int       `json:"challenge_events"`
	ChallengeHash    string    `json:"challenge_hash"`
	AnchorHash       string    `json:"anchor_hash"`
}

func (s *server) integrityReport() integrityReport {
	generatedAt := time.Now().UTC()
	settlementVerification := s.settlement.Verify()
	challengeVerification := s.challenges.Verify()
	anchor := integrityAnchor{
		Version:          "mi-integrity-v1",
		GeneratedAt:      generatedAt,
		SettlementEvents: settlementVerification.Events,
		SettlementHash:   settlementVerification.LastHash,
		ChallengeEvents:  challengeVerification.Events,
		ChallengeHash:    challengeVerification.LastHash,
	}
	anchor.AnchorHash = hashIntegrityAnchor(anchor)
	return integrityReport{
		GeneratedAt: generatedAt,
		Valid:       settlementVerification.Valid && challengeVerification.Valid,
		Settlement:  settlementVerification,
		Challenges:  challengeVerification,
		Anchor:      anchor,
	}
}

func hashIntegrityAnchor(anchor integrityAnchor) string {
	anchor.AnchorHash = ""
	data, _ := json.Marshal(anchor)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func maxRequestBytesOrDefault(configured int64) int64 {
	if configured > 0 {
		return configured
	}
	return defaultMaxRequestBytes
}

// writeDecodeError maps a request-body decode failure to a status code: 413 when
// the body exceeded the configured limit, otherwise 400.
func writeDecodeError(w http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, err.Error(), http.StatusBadRequest)
}

func idempotencyScopedKey(consumerID, key string) string {
	sum := sha256.Sum256([]byte(consumerID + "\x00" + key))
	return hex.EncodeToString(sum[:])
}

func (s *server) adminRunChallenge(w http.ResponseWriter, r *http.Request) {
	var cfg config.ChallengeConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cfg.Model == "" && r.URL.Query().Get("model") != "" {
		cfg.Model = r.URL.Query().Get("model")
	}
	if cfg.ProviderID == "" && r.URL.Query().Get("provider_id") != "" {
		cfg.ProviderID = r.URL.Query().Get("provider_id")
	}
	event, err := s.runSyntheticChallenge(r.Context(), cfg)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, challenge.ErrDisabled) {
			status = http.StatusConflict
		}
		if errors.Is(err, scheduler.ErrNoNode) {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSONStatus(w, http.StatusCreated, event)
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

func (s *server) startChallengeRunner(ctx context.Context, cfg config.ChallengeConfig) {
	if !cfg.Enabled || !cfg.AutoRun {
		return
	}
	interval := cfg.Interval.Duration
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runCtx, cancel := context.WithTimeout(ctx, challengeTimeout(cfg))
				event, err := s.runSyntheticChallenge(runCtx, cfg)
				cancel()
				if err != nil {
					log.Printf("challenge runner: %v", err)
					continue
				}
				log.Printf("challenge runner recorded provider=%s node=%s passed=%t latency_ms=%d score=%d", event.ProviderID, event.NodeID, event.Passed, event.LatencyMs, event.Score)
			}
		}
	}()
}

func (s *server) runSyntheticChallenge(ctx context.Context, cfg config.ChallengeConfig) (challenge.Event, error) {
	model := cfg.Model
	if model == "" {
		models := s.registry.Models()
		if len(models) == 0 {
			return challenge.Event{}, scheduler.ErrNoNode
		}
		model = models[0]
	}
	privacyTier := cfg.PrivacyTier
	if privacyTier == "" {
		privacyTier = privacy.Public
	}
	if _, err := privacy.NormalizeTier(privacyTier); err != nil {
		return challenge.Event{}, err
	}
	prompt := cfg.Prompt
	if prompt == "" {
		prompt = "Answer briefly."
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 12
	}
	ctx, cancel := context.WithTimeout(ctx, challengeTimeout(cfg))
	defer cancel()

	var content string
	startedAt := time.Now()
	inferReq := protocol.InferRequest{
		Model:       model,
		PrivacyTier: privacyTier,
		MaxTokens:   &maxTokens,
		Messages: []protocol.ProtocolMessage{
			{Role: "system", Content: "You are a concise assistant."},
			{Role: "user", Content: prompt},
		},
	}
	sink := collectSink{onChunk: func(chunk string) { content += chunk }}
	providerID := cfg.ProviderID
	if providerID == "" {
		providerID = s.nextSyntheticChallengeProvider(model, privacyTier)
	}
	var result scheduler.DispatchResult
	var err error
	requestID := "chatcmpl-" + randomID()
	if providerID != "" {
		result, err = s.registry.DispatchToProvider(ctx, requestID, providerID, inferReq, sink)
	} else {
		result, err = s.registry.Dispatch(ctx, requestID, inferReq, sink)
	}
	latency := time.Since(startedAt)
	if err != nil {
		if result.ProviderID == "" {
			return challenge.Event{}, err
		}
		return s.challenges.Record(challenge.RecordInput{
			ProviderID: result.ProviderID,
			NodeID:     result.NodeID,
			Challenge:  "synthetic-inference:" + model,
			Passed:     false,
			LatencyMs:  latency.Milliseconds(),
			Score:      0,
			Notes:      "dispatch failed: " + err.Error(),
		})
	}

	expected := strings.TrimSpace(cfg.ExpectedContains)
	trimmed := strings.TrimSpace(content)
	passed := trimmed != ""
	if expected != "" {
		passed = strings.Contains(strings.ToLower(trimmed), strings.ToLower(expected))
	}
	score := 100
	notes := "synthetic inference completed"
	if !passed {
		score = 40
		notes = "synthetic inference response did not match expected output"
	}
	return s.challenges.Record(challenge.RecordInput{
		ProviderID: result.ProviderID,
		NodeID:     result.NodeID,
		Challenge:  "synthetic-inference:" + model,
		Passed:     passed,
		LatencyMs:  latency.Milliseconds(),
		Score:      score,
		Notes:      notes,
	})
}

func (s *server) nextSyntheticChallengeProvider(model string, privacyTier string) string {
	candidates := challengeProviderCandidates(s.registry.Snapshot(), model, privacyTier)
	if len(candidates) == 0 {
		return ""
	}
	s.challengeMu.Lock()
	defer s.challengeMu.Unlock()
	if s.nextChallengeProvider >= len(candidates) {
		s.nextChallengeProvider = 0
	}
	providerID := candidates[s.nextChallengeProvider]
	s.nextChallengeProvider = (s.nextChallengeProvider + 1) % len(candidates)
	return providerID
}

func challengeProviderCandidates(nodes []scheduler.NodeView, model string, privacyTier string) []string {
	seen := map[string]bool{}
	for _, node := range nodes {
		if node.ProviderID == "" || !node.Healthy || node.InCooldown || node.Active >= node.MaxConcurrent {
			continue
		}
		if !containsString(node.Models, model) || !privacy.Accepts(node.PrivacyTiers, privacyTier) {
			continue
		}
		seen[node.ProviderID] = true
	}
	out := make([]string, 0, len(seen))
	for providerID := range seen {
		out = append(out, providerID)
	}
	sort.Strings(out)
	return out
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func challengeTimeout(cfg config.ChallengeConfig) time.Duration {
	if cfg.Timeout.Duration > 0 {
		return cfg.Timeout.Duration
	}
	return 30 * time.Second
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
	if s.maxRequestBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, s.maxRequestBytes)
	}
	var req openai.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDecodeError(w, err)
		return
	}
	if req.Model == "" || len(req.Messages) == 0 {
		http.Error(w, "model and messages are required", http.StatusBadRequest)
		return
	}

	// Bound the request so a stuck node cannot hang the client indefinitely.
	if s.requestTimeout > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), s.requestTimeout)
		defer cancel()
		r = r.WithContext(ctx)
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
		Model:        modelResolution.Target,
		Stream:       req.Stream,
		PrivacyTier:  privacyTier,
		Backend:      requestCapabilityValue(r, "X-Mi-Backend", req.MiBackend),
		DeviceKind:   requestCapabilityValue(r, "X-Mi-Device-Kind", req.MiDeviceKind),
		SoC:          requestCapabilityValue(r, "X-Mi-SoC", req.MiSoC),
		Accelerators: requestAccelerators(r, req),
		Temperature:  req.Temperature,
		MaxTokens:    req.MaxTokens,
	}
	for _, msg := range req.Messages {
		inferReq.Messages = append(inferReq.Messages, protocol.ProtocolMessage{Role: msg.Role, Content: msg.Content})
	}
	applyDefaultMaxTokens(&inferReq)

	cid := consumerID(r.Context())
	var activeIdemKey string
	key := r.Header.Get("Idempotency-Key")
	if key != "" && req.Stream {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"message": "idempotency keys require stream=false",
				"type":    "invalid_request",
			},
		})
		return
	}
	if s.idempotency.Enabled() && key != "" {
		scoped := idempotencyScopedKey(cid, key)
		rec, err := s.idempotency.Begin(scoped)
		if err != nil {
			log.Printf("idempotency begin: %v", err)
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]string{
					"message": "idempotency store error",
					"type":    "internal_error",
				},
			})
			return
		}
		if rec != nil && rec.Status == "completed" {
			w.Header().Set("Content-Type", rec.ContentType)
			w.Header().Set("X-Mi-Idempotent-Replay", "true")
			w.WriteHeader(rec.StatusCode)
			_, _ = w.Write(rec.Response)
			return
		}
		if rec != nil && rec.Status == "in_progress" {
			writeJSONStatus(w, http.StatusConflict, map[string]any{
				"error": map[string]string{
					"message": "a request with this Idempotency-Key is already in progress",
					"type":    "idempotency_conflict",
				},
			})
			return
		}
		activeIdemKey = scoped
	}

	reservation, err := s.market.ReserveConsumerQuota(cid, estimateTokenBudget(req))
	if err != nil {
		if activeIdemKey != "" {
			if err := s.idempotency.Abort(activeIdemKey); err != nil {
				log.Printf("idempotency abort: %v", err)
			}
		}
		writeJSONStatus(w, http.StatusPaymentRequired, map[string]any{
			"error": map[string]string{
				"message": err.Error(),
				"type":    "quota_exceeded",
			},
		})
		return
	}

	if req.Stream {
		s.streamChat(w, r, requestID, req.Model, inferReq, cid, reservation)
		return
	}
	s.blockingChat(w, r, requestID, req.Model, inferReq, cid, reservation, activeIdemKey)
}

func (s *server) embeddings(w http.ResponseWriter, r *http.Request) {
	if s.maxRequestBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, s.maxRequestBytes)
	}
	var req openai.EmbeddingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeDecodeError(w, err)
		return
	}
	input, err := parseEmbeddingInput(req.Input)
	if req.Model == "" || err != nil {
		http.Error(w, "model and non-empty input are required", http.StatusBadRequest)
		return
	}
	if req.EncodingFormat != "" && !strings.EqualFold(req.EncodingFormat, "float") {
		http.Error(w, "only encoding_format=float is supported", http.StatusBadRequest)
		return
	}

	if s.requestTimeout > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), s.requestTimeout)
		defer cancel()
		r = r.WithContext(ctx)
	}

	privacyTier, err := requestEmbeddingPrivacyTier(r, req)
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
	promptTokens := estimateEmbeddingPromptTokens(input)
	cid := consumerID(r.Context())
	reservation, err := s.market.ReserveConsumerQuota(cid, int64(promptTokens))
	if err != nil {
		writeJSONStatus(w, http.StatusPaymentRequired, map[string]any{
			"error": map[string]string{
				"message": err.Error(),
				"type":    "quota_exceeded",
			},
		})
		return
	}

	requestID := "embd-" + randomID()
	embedReq := protocol.EmbedRequest{
		Model:       modelResolution.Target,
		Input:       input,
		PrivacyTier: privacyTier,
	}
	w.Header().Set("X-Mi-Privacy-Tier", privacyTier)
	w.Header().Set("X-Mi-Usage-Source", "coordinator_estimate")
	startedAt := time.Now()
	result, err := s.registry.DispatchEmbedding(r.Context(), requestID, embedReq)
	latency := time.Since(startedAt)
	setEmbeddingDispatchHeaders(w, result)
	if err != nil {
		s.market.ReleaseReservation(reservation)
		status := http.StatusBadGateway
		if errors.Is(err, scheduler.ErrNoNode) {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return
	}

	done := protocol.InferDone{FinishReason: "stop", PromptTokens: promptTokens, OutputTokens: 0}
	if err := s.market.RecordReserved(reservation, cid, result.ProviderID, done); err != nil {
		log.Printf("record usage: %v", err)
	}
	if _, err := s.settlement.Record(settlement.RecordInput{
		RequestID:        requestID,
		ConsumerID:       cid,
		ProviderID:       result.ProviderID,
		NodeID:           result.NodeID,
		Model:            req.Model,
		PrivacyTier:      privacyTier,
		Done:             done,
		Latency:          latency,
		DispatchAttempts: result.Attempts,
	}); err != nil {
		log.Printf("record settlement: %v", err)
	}

	data := make([]openai.EmbeddingData, 0, len(result.Result.Vectors))
	for i, vector := range result.Result.Vectors {
		data = append(data, openai.EmbeddingData{
			Object:    "embedding",
			Index:     i,
			Embedding: vector,
		})
	}
	writeJSON(w, openai.EmbeddingResponse{
		Object: "list",
		Data:   data,
		Model:  req.Model,
		Usage: openai.Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: 0,
			TotalTokens:      promptTokens,
		},
	})
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
	w.Header().Set("X-Mi-Usage-Source", "coordinator_estimate")
	declareDispatchTrailers(w)

	sink := sseSink{w: w, flusher: flusher, requestID: requestID, model: responseModel}
	metered := &meteredSink{inner: &sink}
	startedAt := time.Now()
	result, err := s.registry.Dispatch(r.Context(), requestID, req, metered)
	latency := time.Since(startedAt)
	setDispatchTrailers(w, result)
	if err != nil {
		s.market.ReleaseReservation(reservation)
		s.writeStreamError(w, flusher, err)
		return
	}
	done := coordinatorMeasuredDone(req, result.Done, metered.OutputChars())
	if err := s.market.RecordReserved(reservation, consumerID, result.ProviderID, done); err != nil {
		log.Printf("record usage: %v", err)
	}
	if _, err := s.settlement.Record(settlement.RecordInput{
		RequestID:        requestID,
		ConsumerID:       consumerID,
		ProviderID:       result.ProviderID,
		NodeID:           result.NodeID,
		Model:            responseModel,
		PrivacyTier:      req.PrivacyTier,
		Done:             done,
		Latency:          latency,
		DispatchAttempts: result.Attempts,
	}); err != nil {
		log.Printf("record settlement: %v", err)
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

func (s *server) blockingChat(w http.ResponseWriter, r *http.Request, requestID string, responseModel string, req protocol.InferRequest, consumerID string, reservation *city.QuotaReservation, activeIdemKey string) {
	var content string
	sink := collectSink{onChunk: func(chunk string) { content += chunk }}
	metered := &meteredSink{inner: sink}
	w.Header().Set("X-Mi-Privacy-Tier", req.PrivacyTier)
	w.Header().Set("X-Mi-Usage-Source", "coordinator_estimate")
	startedAt := time.Now()
	result, err := s.registry.Dispatch(r.Context(), requestID, req, metered)
	latency := time.Since(startedAt)
	setDispatchHeaders(w, result)
	if err != nil {
		s.market.ReleaseReservation(reservation)
		if activeIdemKey != "" {
			if err := s.idempotency.Abort(activeIdemKey); err != nil {
				log.Printf("idempotency abort: %v", err)
			}
		}
		status := http.StatusBadGateway
		if errors.Is(err, scheduler.ErrNoNode) {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), status)
		return
	}
	done := coordinatorMeasuredDone(req, result.Done, metered.OutputChars())
	if err := s.market.RecordReserved(reservation, consumerID, result.ProviderID, done); err != nil {
		log.Printf("record usage: %v", err)
	}
	if _, err := s.settlement.Record(settlement.RecordInput{
		RequestID:        requestID,
		ConsumerID:       consumerID,
		ProviderID:       result.ProviderID,
		NodeID:           result.NodeID,
		Model:            responseModel,
		PrivacyTier:      req.PrivacyTier,
		Done:             done,
		Latency:          latency,
		DispatchAttempts: result.Attempts,
	}); err != nil {
		log.Printf("record settlement: %v", err)
	}
	response := openai.ChatCompletionResponse{
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
	}
	body, err := json.Marshal(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	body = append(body, '\n')
	if activeIdemKey != "" {
		if err := s.idempotency.Complete(activeIdemKey, http.StatusOK, "application/json", body); err != nil {
			log.Printf("idempotency complete: %v", err)
			http.Error(w, "idempotency store error", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
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
		case "chunk", "done", "embeddings", "error":
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

	if err := n.write(ctx, protocol.Envelope{Version: protocol.Version, Type: "infer", RequestID: requestID, Infer: &req}); err != nil {
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

func (n *nodeConn) SendEmbedding(ctx context.Context, requestID string, req protocol.EmbedRequest) (protocol.EmbedResult, error) {
	ch := make(chan protocol.Envelope, 128)
	n.mu.Lock()
	n.pending[requestID] = ch
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		delete(n.pending, requestID)
		n.mu.Unlock()
	}()

	if err := n.write(ctx, protocol.Envelope{Version: protocol.Version, Type: "embed", RequestID: requestID, Embed: &req}); err != nil {
		return protocol.EmbedResult{}, err
	}
	for {
		select {
		case <-ctx.Done():
			return protocol.EmbedResult{}, ctx.Err()
		case msg := <-ch:
			switch msg.Type {
			case "embeddings":
				if msg.Embeddings == nil {
					return protocol.EmbedResult{}, errors.New("node returned empty embeddings result")
				}
				return *msg.Embeddings, nil
			case "error":
				if msg.Error != nil {
					return protocol.EmbedResult{}, nodeInferenceError{message: msg.Error.Message, retryable: msg.Error.Retryable}
				}
				return protocol.EmbedResult{}, errors.New("node returned embedding error")
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
			Version:   protocol.Version,
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

type meteredSink struct {
	inner       scheduler.StreamSink
	outputChars int
}

func (s *meteredSink) Chunk(content string) error {
	s.outputChars += utf8.RuneCountInString(content)
	return s.inner.Chunk(content)
}

func (s *meteredSink) OutputChars() int {
	return s.outputChars
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
	if result.Backend != "" {
		w.Header().Set("X-Mi-Backend", result.Backend)
	}
	if result.DeviceKind != "" {
		w.Header().Set("X-Mi-Device-Kind", result.DeviceKind)
	}
	if len(result.Accelerators) > 0 {
		w.Header().Set("X-Mi-Accelerators", strings.Join(result.Accelerators, ","))
	}
	if result.LatencyMs > 0 {
		w.Header().Set("X-Mi-Observed-Latency-Ms", strconv.FormatInt(result.LatencyMs, 10))
	}
	if result.TTFTMs > 0 {
		w.Header().Set("X-Mi-Observed-TTFT-Ms", strconv.FormatInt(result.TTFTMs, 10))
	}
	if result.TokensPerSecond > 0 {
		w.Header().Set("X-Mi-Observed-Tokens-Per-Second", strconv.FormatFloat(result.TokensPerSecond, 'f', 2, 64))
	}
}

func setEmbeddingDispatchHeaders(w http.ResponseWriter, result scheduler.EmbedDispatchResult) {
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
	return requestPrivacyTierValue(r, req.PrivacyTier)
}

func requestEmbeddingPrivacyTier(r *http.Request, req openai.EmbeddingRequest) (string, error) {
	return requestPrivacyTierValue(r, req.PrivacyTier)
}

func requestPrivacyTierValue(r *http.Request, value string) (string, error) {
	if tier := r.Header.Get("X-Mi-Privacy-Tier"); tier != "" {
		return privacy.NormalizeTier(tier)
	}
	return privacy.NormalizeTier(value)
}

func requestCapabilityValue(r *http.Request, header string, value string) string {
	if fromHeader := strings.TrimSpace(r.Header.Get(header)); fromHeader != "" {
		return fromHeader
	}
	return strings.TrimSpace(value)
}

func requestAccelerators(r *http.Request, req openai.ChatCompletionRequest) []string {
	values := append([]string(nil), req.MiAccelerators...)
	if header := r.Header.Get("X-Mi-Accelerator"); header != "" {
		values = append(values, strings.Split(header, ",")...)
	}
	if header := r.Header.Get("X-Mi-Accelerators"); header != "" {
		values = append(values, strings.Split(header, ",")...)
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func coordinatorMeasuredDone(req protocol.InferRequest, reported protocol.InferDone, outputChars int) protocol.InferDone {
	finish := reported.FinishReason
	if finish == "" {
		finish = "stop"
	}
	return protocol.InferDone{
		FinishReason: finish,
		PromptTokens: estimateProtocolPromptTokens(req),
		OutputTokens: estimateTokensFromChars(outputChars),
	}
}

func estimateProtocolPromptTokens(req protocol.InferRequest) int {
	chars := 0
	for _, msg := range req.Messages {
		chars += utf8.RuneCountInString(msg.Role) + utf8.RuneCountInString(msg.Content)
	}
	return estimateTokensFromChars(chars)
}

func parseEmbeddingInput(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" {
		return nil, errors.New("input is required")
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if single == "" {
			return nil, errors.New("input is required")
		}
		return []string{single}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, err
	}
	if len(many) == 0 {
		return nil, errors.New("input is required")
	}
	for _, value := range many {
		if value == "" {
			return nil, errors.New("input is required")
		}
	}
	return many, nil
}

func estimateEmbeddingPromptTokens(input []string) int {
	chars := 0
	for _, text := range input {
		chars += utf8.RuneCountInString(text)
	}
	return estimateTokensFromChars(chars)
}

func estimateTokensFromChars(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + 3) / 4
}

func estimateTokenBudget(req openai.ChatCompletionRequest) int64 {
	chars := 0
	for _, msg := range req.Messages {
		chars += utf8.RuneCountInString(msg.Role) + utf8.RuneCountInString(msg.Content)
	}
	promptEstimate := int64(estimateTokensFromChars(chars))
	outputEstimate := int64(defaultReservedOutputTokens)
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		outputEstimate = int64(*req.MaxTokens)
	}
	return promptEstimate + outputEstimate
}

func applyDefaultMaxTokens(req *protocol.InferRequest) {
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		return
	}
	defaultMaxTokens := defaultReservedOutputTokens
	req.MaxTokens = &defaultMaxTokens
}

func declareDispatchTrailers(w http.ResponseWriter) {
	w.Header().Add("Trailer", "X-Mi-Dispatch-Attempts")
	w.Header().Add("Trailer", "X-Mi-Node-Id")
	w.Header().Add("Trailer", "X-Mi-Provider-Id")
	w.Header().Add("Trailer", "X-Mi-Backend")
	w.Header().Add("Trailer", "X-Mi-Device-Kind")
	w.Header().Add("Trailer", "X-Mi-Accelerators")
	w.Header().Add("Trailer", "X-Mi-Observed-Latency-Ms")
	w.Header().Add("Trailer", "X-Mi-Observed-TTFT-Ms")
	w.Header().Add("Trailer", "X-Mi-Observed-Tokens-Per-Second")
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

// newHTTPServer builds the coordinator's *http.Server and validates the TLS
// configuration up front, so the caller holds the server reference and can
// drive a graceful Shutdown. It does not start listening.
func newHTTPServer(cfg config.Coordinator, handler http.Handler) (*http.Server, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.TLS.NodeClientCAFile != "" {
		certPool, err := loadCertPool(cfg.TLS.NodeClientCAFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.ClientCAs = certPool
		tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
	}
	if cfg.TLS.CertFile != "" || cfg.TLS.KeyFile != "" {
		if cfg.TLS.CertFile == "" || cfg.TLS.KeyFile == "" {
			return nil, errors.New("both tls.cert_file and tls.key_file are required")
		}
	} else if cfg.TLS.NodeClientCAFile != "" {
		return nil, errors.New("tls.node_client_ca_file requires tls.cert_file and tls.key_file")
	}
	return &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: handler,
		// Keep WebSocket upgrades on HTTP/1.1 until the provider wire protocol
		// explicitly supports RFC 8441 WebSockets over HTTP/2.
		TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){},
		TLSConfig:    tlsConfig,
	}, nil
}

// listenAndServe blocks serving on the configured listener. It returns
// http.ErrServerClosed after a graceful Shutdown, which the caller treats as a
// clean stop rather than a failure.
func listenAndServe(server *http.Server, cfg config.Coordinator) error {
	if cfg.TLS.CertFile != "" {
		log.Printf("TLS enabled for coordinator")
		return server.ListenAndServeTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile)
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
