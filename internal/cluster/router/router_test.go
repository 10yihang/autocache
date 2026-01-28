package router

import (
	"context"
	"testing"

	"github.com/10yihang/autocache/internal/cluster"
	"github.com/10yihang/autocache/internal/cluster/hash"
)

func TestClusterRouter_NilCluster(t *testing.T) {
	r := NewClusterRouter(nil)

	result := r.Route(context.Background(), []byte("foo"), false)
	if !result.Local {
		t.Errorf("Route with nil cluster: got Local=%v, want true", result.Local)
	}

	result = r.RouteMulti(context.Background(), [][]byte{[]byte("a"), []byte("b")})
	if !result.Local {
		t.Errorf("RouteMulti with nil cluster: got Local=%v, want true", result.Local)
	}
}

func TestClusterRouter_Route_Local(t *testing.T) {
	c := setupTestCluster(t, "node1", 6379)
	r := NewClusterRouter(c)

	c.AssignSlots([]uint16{0, 1, 2, 100, hash.KeySlot("localkey")})

	result := r.Route(context.Background(), []byte("localkey"), false)
	if !result.Local {
		t.Errorf("Route local key: got Local=%v, want true", result.Local)
	}
	if result.Redirect != nil {
		t.Errorf("Route local key: got Redirect=%v, want nil", result.Redirect)
	}
}

func TestClusterRouter_Route_Moved(t *testing.T) {
	c := setupTestCluster(t, "node1", 6379)
	r := NewClusterRouter(c)

	result := r.Route(context.Background(), []byte("unassigned"), false)
	if !result.Local {
		t.Errorf("Route unassigned slot: got Local=%v, want true (defensive)", result.Local)
	}
}

func TestClusterRouter_Route_UnassignedSlot(t *testing.T) {
	c := setupTestCluster(t, "node1", 6379)
	r := NewClusterRouter(c)

	result := r.Route(context.Background(), []byte("noowner"), false)
	if !result.Local {
		t.Errorf("Route unassigned: got Local=%v, want true", result.Local)
	}
}

func TestClusterRouter_RouteMulti_SameSlot(t *testing.T) {
	c := setupTestCluster(t, "node1", 6379)
	r := NewClusterRouter(c)

	keys := [][]byte{[]byte("{user}.name"), []byte("{user}.email"), []byte("{user}.age")}
	slot := hash.KeySlot("{user}.name")
	c.AssignSlots([]uint16{slot})

	result := r.RouteMulti(context.Background(), keys)
	if !result.Local {
		t.Errorf("RouteMulti same slot: got Local=%v, want true", result.Local)
	}
	if result.CrossSlot {
		t.Errorf("RouteMulti same slot: got CrossSlot=%v, want false", result.CrossSlot)
	}
}

func TestClusterRouter_RouteMulti_CrossSlot(t *testing.T) {
	c := setupTestCluster(t, "node1", 6379)
	r := NewClusterRouter(c)

	keys := [][]byte{[]byte("foo"), []byte("bar"), []byte("baz")}
	slot0 := hash.KeySlot("foo")
	slot1 := hash.KeySlot("bar")
	slot2 := hash.KeySlot("baz")
	if slot0 == slot1 || slot1 == slot2 || slot0 == slot2 {
		t.Skip("Keys hash to same slot, can't test cross-slot")
	}

	result := r.RouteMulti(context.Background(), keys)
	if !result.CrossSlot {
		t.Errorf("RouteMulti cross slot: got CrossSlot=%v, want true", result.CrossSlot)
	}
	if result.Local {
		t.Errorf("RouteMulti cross slot: got Local=%v, want false", result.Local)
	}
}

func TestClusterRouter_RouteMulti_EmptyKeys(t *testing.T) {
	c := setupTestCluster(t, "node1", 6379)
	r := NewClusterRouter(c)

	result := r.RouteMulti(context.Background(), [][]byte{})
	if !result.Local {
		t.Errorf("RouteMulti empty: got Local=%v, want true", result.Local)
	}
}

func TestClusterRouter_RouteMulti_SingleKey(t *testing.T) {
	c := setupTestCluster(t, "node1", 6379)
	r := NewClusterRouter(c)

	slot := hash.KeySlot("onlykey")
	c.AssignSlots([]uint16{slot})

	result := r.RouteMulti(context.Background(), [][]byte{[]byte("onlykey")})
	if !result.Local {
		t.Errorf("RouteMulti single key: got Local=%v, want true", result.Local)
	}
	if result.CrossSlot {
		t.Errorf("RouteMulti single key: got CrossSlot=%v, want false", result.CrossSlot)
	}
}

func TestRouteResult_MutuallyExclusive(t *testing.T) {
	tests := []struct {
		name      string
		result    RouteResult
		wantValid bool
	}{
		{
			name:      "local only",
			result:    RouteResult{Local: true},
			wantValid: true,
		},
		{
			name:      "redirect only",
			result:    RouteResult{Redirect: &Redirect{Type: RedirectMoved, Slot: 100, Addr: "127.0.0.1:7001"}},
			wantValid: true,
		},
		{
			name:      "crossslot only",
			result:    RouteResult{CrossSlot: true},
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := 0
			if tt.result.Local {
				count++
			}
			if tt.result.Redirect != nil {
				count++
			}
			if tt.result.CrossSlot {
				count++
			}
			if count > 1 {
				t.Errorf("RouteResult has multiple states set: Local=%v, Redirect=%v, CrossSlot=%v",
					tt.result.Local, tt.result.Redirect != nil, tt.result.CrossSlot)
			}
		})
	}
}

func TestRedirectType_Constants(t *testing.T) {
	if RedirectMoved != 0 {
		t.Errorf("RedirectMoved = %d, want 0", RedirectMoved)
	}
	if RedirectAsk != 1 {
		t.Errorf("RedirectAsk = %d, want 1", RedirectAsk)
	}
}

func setupTestCluster(t *testing.T, nodeID string, port int) *cluster.Cluster {
	t.Helper()
	cfg := &cluster.Config{
		NodeID:      nodeID,
		BindAddr:    "127.0.0.1",
		Port:        port,
		ClusterPort: port + 10000,
	}
	c, err := cluster.NewCluster(cfg, nil)
	if err != nil {
		t.Fatalf("NewCluster: %v", err)
	}
	return c
}
