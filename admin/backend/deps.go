package admin

import (
	"time"

	"github.com/10yihang/autocache/internal/cluster"
	"github.com/10yihang/autocache/internal/cluster/hotspot"
	"github.com/10yihang/autocache/internal/cluster/replication"
	memstore "github.com/10yihang/autocache/internal/engine/memory"
	"github.com/10yihang/autocache/internal/engine/tiered"
	"github.com/10yihang/autocache/internal/protocol"
)

type Deps struct {
	Store           *memstore.Store
	Cluster         *cluster.Cluster
	Tiered          *tiered.Manager
	Replication     *replication.Manager
	ProtocolHandler *protocol.Handler
	Hotspot         *hotspot.Detector
	Version         string
	GoVersion       string
	StartedAt       time.Time
}
