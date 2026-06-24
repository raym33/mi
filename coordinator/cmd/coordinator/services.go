package main

import (
	"context"

	"github.com/raym33/mi/internal/challenge"
	"github.com/raym33/mi/internal/city"
	"github.com/raym33/mi/internal/modelcatalog"
	"github.com/raym33/mi/internal/protocol"
	"github.com/raym33/mi/internal/scheduler"
	"github.com/raym33/mi/internal/settlement"
)

// The coordinator depends on these interfaces, not on the concrete subsystem
// types. They are defined here, on the consumer side, and capture exactly the
// methods the coordinator uses — no more. This documents the internal seams of
// the control plane (see ARCHITECTURE.md), keeps each subsystem swappable and
// independently mockable in tests, and marks the boundaries along which the
// process would be split if it ever needed to be. This is a behaviour-neutral
// decomposition: the concrete types are unchanged and still wired in main.

// nodeRegistry is the scheduling and membership surface: node lifecycle,
// inspection, reputation feed, and request dispatch.
type nodeRegistry interface {
	Register(msg protocol.Register, conn scheduler.NodeConn)
	Heartbeat(msg protocol.Heartbeat)
	Remove(nodeID string)
	RemoveProvider(providerID string) int
	Models() []string
	Snapshot() []scheduler.NodeView
	NetworkStatus() scheduler.NetworkStatus
	SetProviderScores(scores map[string]int)
	Dispatch(ctx context.Context, requestID string, req protocol.InferRequest, sink scheduler.StreamSink) (scheduler.DispatchResult, error)
	DispatchToProvider(ctx context.Context, requestID string, providerID string, req protocol.InferRequest, sink scheduler.StreamSink) (scheduler.DispatchResult, error)
}

// accountMarket is the consumer/provider account, quota, and privacy-policy
// surface, including pre-dispatch quota reservation and usage recording.
type accountMarket interface {
	AuthenticateConsumer(key string) (string, error)
	AuthenticateProvider(reg protocol.Register) (string, error)
	CheckConsumerQuota(accountID string) error
	ConsumerStatus(accountID string) city.ConsumerStatus
	CreateConsumer(input city.CreateConsumerInput) (city.CreatedConsumer, error)
	CreateProvider(input city.CreateProviderInput) (city.CreatedProvider, error)
	DisableConsumer(rawID string) (city.Consumer, error)
	DisableProvider(rawID string) (city.Provider, error)
	EnforceProviderPrivacy(providerID string, requestedMode string, requestedTiers []string) (string, []string, error)
	ReserveConsumerQuota(accountID string, estimatedTokens int64) (*city.QuotaReservation, error)
	ReleaseReservation(reservation *city.QuotaReservation)
	RecordReserved(reservation *city.QuotaReservation, consumerID, providerID string, done protocol.InferDone) error
	RotateConsumerKey(rawID string) (city.CreatedConsumer, error)
	RotateProviderToken(rawID string) (city.CreatedProvider, error)
	Snapshot() city.Snapshot
}

// settlementLedger is the cooperative accounting surface: a tamper-evident,
// hash-chained log of usage and provider rewards.
type settlementLedger interface {
	Record(input settlement.RecordInput) (settlement.Event, error)
	Snapshot(limit int) settlement.Snapshot
	Verify() settlement.Verification
}

// challengeLedger is the benchmark challenge evidence surface.
type challengeLedger interface {
	Record(input challenge.RecordInput) (challenge.Event, error)
	Snapshot(limit int) challenge.Snapshot
	Verify() challenge.Verification
}

// catalogService resolves model aliases and exposes the visible model catalog.
type catalogService interface {
	Resolve(model string) modelcatalog.Resolution
	VisibleModelIDs(available []string) []string
	Catalog(available []string) modelcatalog.CatalogResponse
}

// Compile-time assertions that the production implementations satisfy the
// coordinator's service interfaces. If a subsystem's signature drifts, this
// fails to build rather than failing at runtime.
var (
	_ nodeRegistry     = (*scheduler.Registry)(nil)
	_ accountMarket    = (*city.Market)(nil)
	_ settlementLedger = (*settlement.Ledger)(nil)
	_ challengeLedger  = (*challenge.Ledger)(nil)
	_ catalogService   = (*modelcatalog.Catalog)(nil)
)
