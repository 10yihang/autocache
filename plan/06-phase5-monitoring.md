# Phase 5: 监控运维与论文收尾

## 目标

- 实现完整的 Prometheus 指标体系
- 构建 Grafana 可视化面板
- 完成性能测试与数据收集
- 撰写毕业论文并准备答辩

## 时间：2026.05.19 - 2026.06.12（3.5 周）

---

## 架构设计

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          监控运维体系                                    │
│                                                                          │
│  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐     │
│  │   AutoCache     │    │   Prometheus    │    │    Grafana      │     │
│  │   (Metrics)     │───►│   (收集存储)     │───►│   (可视化)       │     │
│  │   :9121/metrics │    │                 │    │                 │     │
│  └─────────────────┘    └─────────────────┘    └─────────────────┘     │
│           │                      │                      │               │
│           │                      ▼                      │               │
│           │             ┌─────────────────┐             │               │
│           │             │  AlertManager   │             │               │
│           │             │   (告警管理)     │             │               │
│           │             └─────────────────┘             │               │
│           │                      │                      │               │
│           ▼                      ▼                      ▼               │
│    ┌──────────────────────────────────────────────────────────┐        │
│    │                    监控指标类型                           │        │
│    │  • 性能指标：QPS、延迟、命中率                            │        │
│    │  • 资源指标：CPU、内存、网络、磁盘                        │        │
│    │  • 业务指标：连接数、Key数、过期数                        │        │
│    │  • 集群指标：节点状态、Slot分布、迁移进度                 │        │
│    │  • 分层指标：各层数据量、迁移统计                         │        │
│    └──────────────────────────────────────────────────────────┘        │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Week 1: Prometheus 指标实现

### 1.1 指标定义

#### internal/metrics/metrics.go
```go
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "autocache"
)

var (
	// =============== 性能指标 ===============
	
	// CommandsTotal 命令总数
	CommandsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "commands_total",
			Help:      "Total number of commands processed",
		},
		[]string{"cmd", "status"}, // cmd: get/set/del, status: success/error
	)
	
	// CommandDuration 命令延迟
	CommandDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "command_duration_seconds",
			Help:      "Command latency in seconds",
			Buckets:   []float64{.0001, .0005, .001, .005, .01, .05, .1, .5, 1},
		},
		[]string{"cmd"},
	)
	
	// CacheHits 缓存命中
	CacheHits = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_hits_total",
			Help:      "Total number of cache hits",
		},
	)
	
	// CacheMisses 缓存未命中
	CacheMisses = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_misses_total",
			Help:      "Total number of cache misses",
		},
	)
	
	// =============== 资源指标 ===============
	
	// MemoryUsage 内存使用
	MemoryUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "memory_bytes",
			Help:      "Memory usage in bytes",
		},
		[]string{"type"}, // used/rss/peak
	)
	
	// KeysTotal 总 Key 数
	KeysTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "keys_total",
			Help:      "Total number of keys",
		},
	)
	
	// KeysExpired 过期 Key 数
	KeysExpired = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "keys_expired_total",
			Help:      "Total number of expired keys",
		},
	)
	
	// KeysEvicted 淘汰 Key 数
	KeysEvicted = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "keys_evicted_total",
			Help:      "Total number of evicted keys",
		},
	)
	
	// =============== 连接指标 ===============
	
	// ConnectionsTotal 连接总数
	ConnectionsTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "connections_total",
			Help:      "Total number of client connections",
		},
	)
	
	// ConnectionsReceived 累计接收连接数
	ConnectionsReceived = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "connections_received_total",
			Help:      "Total number of connections received",
		},
	)
	
	// ConnectionsRejected 拒绝连接数
	ConnectionsRejected = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "connections_rejected_total",
			Help:      "Total number of connections rejected",
		},
	)
	
	// =============== 集群指标 ===============
	
	// ClusterState 集群状态
	ClusterState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cluster_state",
			Help:      "Cluster state (0=fail, 1=ok)",
		},
		[]string{"cluster"},
	)
	
	// ClusterNodesTotal 集群节点数
	ClusterNodesTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cluster_nodes_total",
			Help:      "Total number of cluster nodes",
		},
		[]string{"role"}, // master/replica
	)
	
	// ClusterSlotsAssigned 已分配 Slot 数
	ClusterSlotsAssigned = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cluster_slots_assigned",
			Help:      "Number of slots assigned",
		},
	)
	
	// ClusterSlotsMigrating 迁移中的 Slot 数
	ClusterSlotsMigrating = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cluster_slots_migrating",
			Help:      "Number of slots being migrated",
		},
	)
	
	// =============== 分层存储指标 ===============
	
	// TieredKeys 各层 Key 数
	TieredKeys = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "tiered_keys_total",
			Help:      "Number of keys in each tier",
		},
		[]string{"tier"}, // hot/warm/cold
	)
	
	// TieredBytes 各层数据量
	TieredBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "tiered_bytes",
			Help:      "Data size in each tier",
		},
		[]string{"tier"},
	)
	
	// TieredMigrations 迁移次数
	TieredMigrations = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "tiered_migrations_total",
			Help:      "Total number of tier migrations",
		},
		[]string{"direction"}, // promote/demote
	)
	
	// TieredHitRate 各层命中率
	TieredHitRate = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "tiered_hit_rate",
			Help:      "Hit rate for each tier",
		},
		[]string{"tier"},
	)
	
	// =============== 持久化指标 ===============
	
	// RDBLastSaveTime RDB 最后保存时间
	RDBLastSaveTime = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "rdb_last_save_timestamp",
			Help:      "Timestamp of last RDB save",
		},
	)
	
	// RDBSaveDuration RDB 保存耗时
	RDBSaveDuration = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "rdb_save_duration_seconds",
			Help:      "Duration of last RDB save",
		},
	)
	
	// AOFCurrentSize AOF 文件大小
	AOFCurrentSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "aof_current_size_bytes",
			Help:      "Current AOF file size",
		},
	)
	
	// =============== 网络指标 ===============
	
	// NetworkBytesReceived 接收字节数
	NetworkBytesReceived = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "network_bytes_received_total",
			Help:      "Total bytes received",
		},
	)
	
	// NetworkBytesSent 发送字节数
	NetworkBytesSent = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "network_bytes_sent_total",
			Help:      "Total bytes sent",
		},
	)
	
	// =============== 服务信息 ===============
	
	// Info 服务信息
	Info = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "info",
			Help:      "AutoCache server info",
		},
		[]string{"version", "go_version", "os", "arch"},
	)
	
	// Uptime 运行时间
	Uptime = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "uptime_seconds",
			Help:      "Server uptime in seconds",
		},
	)
)

// InitInfo 初始化服务信息
func InitInfo(version, goVersion, os, arch string) {
	Info.WithLabelValues(version, goVersion, os, arch).Set(1)
}
```

### 1.2 指标收集器

#### internal/metrics/collector.go
```go
package metrics

import (
	"runtime"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Collector 指标收集器
type Collector struct {
	startTime time.Time
	
	// 引用存储引擎等组件
	engine  interface{}
	cluster interface{}
	tiered  interface{}
	
	mu sync.RWMutex
}

// NewCollector 创建收集器
func NewCollector() *Collector {
	return &Collector{
		startTime: time.Now(),
	}
}

// SetEngine 设置存储引擎
func (c *Collector) SetEngine(engine interface{}) {
	c.mu.Lock()
	c.engine = engine
	c.mu.Unlock()
}

// SetCluster 设置集群
func (c *Collector) SetCluster(cluster interface{}) {
	c.mu.Lock()
	c.cluster = cluster
	c.mu.Unlock()
}

// SetTiered 设置分层管理器
func (c *Collector) SetTiered(tiered interface{}) {
	c.mu.Lock()
	c.tiered = tiered
	c.mu.Unlock()
}

// Collect 收集指标
func (c *Collector) Collect() {
	c.collectMemory()
	c.collectUptime()
	// c.collectEngine()
	// c.collectCluster()
	// c.collectTiered()
}

// collectMemory 收集内存指标
func (c *Collector) collectMemory() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	MemoryUsage.WithLabelValues("alloc").Set(float64(m.Alloc))
	MemoryUsage.WithLabelValues("sys").Set(float64(m.Sys))
	MemoryUsage.WithLabelValues("heap_alloc").Set(float64(m.HeapAlloc))
	MemoryUsage.WithLabelValues("heap_sys").Set(float64(m.HeapSys))
	MemoryUsage.WithLabelValues("heap_inuse").Set(float64(m.HeapInuse))
}

// collectUptime 收集运行时间
func (c *Collector) collectUptime() {
	Uptime.Set(time.Since(c.startTime).Seconds())
}

// RecordCommand 记录命令执行
func RecordCommand(cmd string, duration time.Duration, success bool) {
	status := "success"
	if !success {
		status = "error"
	}
	
	CommandsTotal.WithLabelValues(cmd, status).Inc()
	CommandDuration.WithLabelValues(cmd).Observe(duration.Seconds())
}

// RecordCacheHit 记录缓存命中
func RecordCacheHit() {
	CacheHits.Inc()
}

// RecordCacheMiss 记录缓存未命中
func RecordCacheMiss() {
	CacheMisses.Inc()
}

// RecordConnection 记录连接
func RecordConnection(delta int) {
	ConnectionsTotal.Add(float64(delta))
	if delta > 0 {
		ConnectionsReceived.Inc()
	}
}

// RecordTieredMigration 记录分层迁移
func RecordTieredMigration(direction string) {
	TieredMigrations.WithLabelValues(direction).Inc()
}
```

### 1.3 HTTP 端点

#### internal/metrics/exporter.go
```go
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Exporter 指标导出器
type Exporter struct {
	addr      string
	collector *Collector
	server    *http.Server
}

// NewExporter 创建导出器
func NewExporter(addr string) *Exporter {
	collector := NewCollector()
	
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/ready", readyHandler)
	
	return &Exporter{
		addr:      addr,
		collector: collector,
		server: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}
}

// Start 启动导出器
func (e *Exporter) Start() error {
	// 启动定时收集
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		
		for range ticker.C {
			e.collector.Collect()
		}
	}()
	
	return e.server.ListenAndServe()
}

// Stop 停止导出器
func (e *Exporter) Stop() error {
	return e.server.Close()
}

// GetCollector 获取收集器
func (e *Exporter) GetCollector() *Collector {
	return e.collector
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func readyHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: 检查服务是否就绪
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Ready"))
}
```

---

## Week 2: Grafana Dashboard

### 2.1 Dashboard JSON

#### deploy/grafana/autocache-dashboard.json
```json
{
  "annotations": {
    "list": []
  },
  "editable": true,
  "fiscalYearStartMonth": 0,
  "graphTooltip": 0,
  "id": null,
  "links": [],
  "liveNow": false,
  "panels": [
    {
      "collapsed": false,
      "gridPos": {"h": 1, "w": 24, "x": 0, "y": 0},
      "id": 1,
      "panels": [],
      "title": "概览",
      "type": "row"
    },
    {
      "datasource": {"type": "prometheus", "uid": "${datasource}"},
      "fieldConfig": {
        "defaults": {
          "color": {"mode": "thresholds"},
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {"color": "green", "value": null}
            ]
          },
          "unit": "short"
        }
      },
      "gridPos": {"h": 4, "w": 4, "x": 0, "y": 1},
      "id": 2,
      "options": {
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto",
        "orientation": "auto",
        "reduceOptions": {
          "calcs": ["lastNotNull"],
          "fields": "",
          "values": false
        },
        "textMode": "auto"
      },
      "targets": [
        {
          "expr": "autocache_keys_total",
          "legendFormat": "",
          "refId": "A"
        }
      ],
      "title": "总 Key 数",
      "type": "stat"
    },
    {
      "datasource": {"type": "prometheus", "uid": "${datasource}"},
      "fieldConfig": {
        "defaults": {
          "color": {"mode": "thresholds"},
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {"color": "green", "value": null}
            ]
          },
          "unit": "ops"
        }
      },
      "gridPos": {"h": 4, "w": 4, "x": 4, "y": 1},
      "id": 3,
      "options": {
        "colorMode": "value",
        "graphMode": "area",
        "justifyMode": "auto",
        "orientation": "auto",
        "reduceOptions": {
          "calcs": ["lastNotNull"],
          "fields": "",
          "values": false
        },
        "textMode": "auto"
      },
      "targets": [
        {
          "expr": "sum(rate(autocache_commands_total[1m]))",
          "legendFormat": "",
          "refId": "A"
        }
      ],
      "title": "QPS",
      "type": "stat"
    },
    {
      "datasource": {"type": "prometheus", "uid": "${datasource}"},
      "fieldConfig": {
        "defaults": {
          "color": {"mode": "thresholds"},
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {"color": "green", "value": null},
              {"color": "yellow", "value": 0.001},
              {"color": "red", "value": 0.01}
            ]
          },
          "unit": "s"
        }
      },
      "gridPos": {"h": 4, "w": 4, "x": 8, "y": 1},
      "id": 4,
      "options": {
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto",
        "orientation": "auto",
        "reduceOptions": {
          "calcs": ["lastNotNull"],
          "fields": "",
          "values": false
        },
        "textMode": "auto"
      },
      "targets": [
        {
          "expr": "histogram_quantile(0.99, sum(rate(autocache_command_duration_seconds_bucket[5m])) by (le))",
          "legendFormat": "",
          "refId": "A"
        }
      ],
      "title": "P99 延迟",
      "type": "stat"
    },
    {
      "datasource": {"type": "prometheus", "uid": "${datasource}"},
      "fieldConfig": {
        "defaults": {
          "color": {"mode": "thresholds"},
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [
              {"color": "red", "value": null},
              {"color": "yellow", "value": 0.9},
              {"color": "green", "value": 0.95}
            ]
          },
          "unit": "percentunit"
        }
      },
      "gridPos": {"h": 4, "w": 4, "x": 12, "y": 1},
      "id": 5,
      "options": {
        "colorMode": "value",
        "graphMode": "none",
        "justifyMode": "auto",
        "orientation": "auto",
        "reduceOptions": {
          "calcs": ["lastNotNull"],
          "fields": "",
          "values": false
        },
        "textMode": "auto"
      },
      "targets": [
        {
          "expr": "autocache_cache_hits_total / (autocache_cache_hits_total + autocache_cache_misses_total)",
          "legendFormat": "",
          "refId": "A"
        }
      ],
      "title": "缓存命中率",
      "type": "stat"
    },
    {
      "datasource": {"type": "prometheus", "uid": "${datasource}"},
      "fieldConfig": {
        "defaults": {
          "color": {"mode": "palette-classic"},
          "custom": {
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "drawStyle": "line",
            "fillOpacity": 10,
            "gradientMode": "none",
            "hideFrom": {"legend": false, "tooltip": false, "viz": false},
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {"type": "linear"},
            "showPoints": "never",
            "spanNulls": false,
            "stacking": {"group": "A", "mode": "none"},
            "thresholdsStyle": {"mode": "off"}
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [{"color": "green", "value": null}]
          },
          "unit": "ops"
        }
      },
      "gridPos": {"h": 8, "w": 12, "x": 0, "y": 5},
      "id": 6,
      "options": {
        "legend": {"calcs": ["mean", "max"], "displayMode": "table", "placement": "bottom", "showLegend": true},
        "tooltip": {"mode": "multi", "sort": "none"}
      },
      "targets": [
        {
          "expr": "sum(rate(autocache_commands_total{cmd=\"get\"}[1m]))",
          "legendFormat": "GET",
          "refId": "A"
        },
        {
          "expr": "sum(rate(autocache_commands_total{cmd=\"set\"}[1m]))",
          "legendFormat": "SET",
          "refId": "B"
        },
        {
          "expr": "sum(rate(autocache_commands_total{cmd=\"del\"}[1m]))",
          "legendFormat": "DEL",
          "refId": "C"
        }
      ],
      "title": "命令 QPS",
      "type": "timeseries"
    },
    {
      "datasource": {"type": "prometheus", "uid": "${datasource}"},
      "fieldConfig": {
        "defaults": {
          "color": {"mode": "palette-classic"},
          "custom": {
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "drawStyle": "line",
            "fillOpacity": 10,
            "gradientMode": "none",
            "hideFrom": {"legend": false, "tooltip": false, "viz": false},
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {"type": "linear"},
            "showPoints": "never",
            "spanNulls": false,
            "stacking": {"group": "A", "mode": "none"},
            "thresholdsStyle": {"mode": "off"}
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [{"color": "green", "value": null}]
          },
          "unit": "s"
        }
      },
      "gridPos": {"h": 8, "w": 12, "x": 12, "y": 5},
      "id": 7,
      "options": {
        "legend": {"calcs": ["mean", "max"], "displayMode": "table", "placement": "bottom", "showLegend": true},
        "tooltip": {"mode": "multi", "sort": "none"}
      },
      "targets": [
        {
          "expr": "histogram_quantile(0.50, sum(rate(autocache_command_duration_seconds_bucket[5m])) by (le, cmd))",
          "legendFormat": "{{cmd}} P50",
          "refId": "A"
        },
        {
          "expr": "histogram_quantile(0.99, sum(rate(autocache_command_duration_seconds_bucket[5m])) by (le, cmd))",
          "legendFormat": "{{cmd}} P99",
          "refId": "B"
        }
      ],
      "title": "命令延迟",
      "type": "timeseries"
    },
    {
      "collapsed": false,
      "gridPos": {"h": 1, "w": 24, "x": 0, "y": 13},
      "id": 10,
      "panels": [],
      "title": "冷热分层",
      "type": "row"
    },
    {
      "datasource": {"type": "prometheus", "uid": "${datasource}"},
      "fieldConfig": {
        "defaults": {
          "color": {"mode": "palette-classic"},
          "custom": {
            "hideFrom": {"legend": false, "tooltip": false, "viz": false}
          },
          "mappings": []
        }
      },
      "gridPos": {"h": 8, "w": 8, "x": 0, "y": 14},
      "id": 11,
      "options": {
        "legend": {"displayMode": "list", "placement": "right", "showLegend": true},
        "pieType": "pie",
        "reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": false},
        "tooltip": {"mode": "single", "sort": "none"}
      },
      "targets": [
        {
          "expr": "autocache_tiered_keys_total",
          "legendFormat": "{{tier}}",
          "refId": "A"
        }
      ],
      "title": "各层 Key 分布",
      "type": "piechart"
    },
    {
      "datasource": {"type": "prometheus", "uid": "${datasource}"},
      "fieldConfig": {
        "defaults": {
          "color": {"mode": "palette-classic"},
          "custom": {
            "axisCenteredZero": false,
            "axisColorMode": "text",
            "axisLabel": "",
            "axisPlacement": "auto",
            "barAlignment": 0,
            "drawStyle": "line",
            "fillOpacity": 10,
            "gradientMode": "none",
            "hideFrom": {"legend": false, "tooltip": false, "viz": false},
            "lineInterpolation": "linear",
            "lineWidth": 1,
            "pointSize": 5,
            "scaleDistribution": {"type": "linear"},
            "showPoints": "never",
            "spanNulls": false,
            "stacking": {"group": "A", "mode": "none"},
            "thresholdsStyle": {"mode": "off"}
          },
          "mappings": [],
          "thresholds": {
            "mode": "absolute",
            "steps": [{"color": "green", "value": null}]
          },
          "unit": "short"
        }
      },
      "gridPos": {"h": 8, "w": 16, "x": 8, "y": 14},
      "id": 12,
      "options": {
        "legend": {"calcs": ["mean", "max"], "displayMode": "table", "placement": "bottom", "showLegend": true},
        "tooltip": {"mode": "multi", "sort": "none"}
      },
      "targets": [
        {
          "expr": "rate(autocache_tiered_migrations_total{direction=\"promote\"}[5m])",
          "legendFormat": "升级 (Promote)",
          "refId": "A"
        },
        {
          "expr": "rate(autocache_tiered_migrations_total{direction=\"demote\"}[5m])",
          "legendFormat": "降级 (Demote)",
          "refId": "B"
        }
      ],
      "title": "数据迁移速率",
      "type": "timeseries"
    }
  ],
  "refresh": "10s",
  "schemaVersion": 38,
  "style": "dark",
  "tags": ["autocache", "cache", "kubernetes"],
  "templating": {
    "list": [
      {
        "current": {"selected": false, "text": "Prometheus", "value": "prometheus"},
        "hide": 0,
        "includeAll": false,
        "label": "Datasource",
        "multi": false,
        "name": "datasource",
        "options": [],
        "query": "prometheus",
        "refresh": 1,
        "regex": "",
        "skipUrlSync": false,
        "type": "datasource"
      }
    ]
  },
  "time": {"from": "now-1h", "to": "now"},
  "timepicker": {},
  "timezone": "",
  "title": "AutoCache Dashboard",
  "uid": "autocache-main",
  "version": 1,
  "weekStart": ""
}
```

### 2.2 告警规则

#### deploy/prometheus/autocache-alerts.yaml
```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: autocache-alerts
  namespace: monitoring
  labels:
    app: autocache
spec:
  groups:
    - name: autocache.rules
      rules:
        # 高延迟告警
        - alert: AutoCacheHighLatency
          expr: histogram_quantile(0.99, sum(rate(autocache_command_duration_seconds_bucket[5m])) by (le)) > 0.01
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "AutoCache P99 延迟过高"
            description: "P99 延迟超过 10ms，当前值: {{ $value | humanizeDuration }}"
        
        # 低命中率告警
        - alert: AutoCacheLowHitRate
          expr: |
            (autocache_cache_hits_total / (autocache_cache_hits_total + autocache_cache_misses_total)) < 0.8
          for: 10m
          labels:
            severity: warning
          annotations:
            summary: "AutoCache 命中率过低"
            description: "缓存命中率低于 80%，当前值: {{ $value | humanizePercentage }}"
        
        # 节点下线告警
        - alert: AutoCacheNodeDown
          expr: autocache_cluster_state == 0
          for: 1m
          labels:
            severity: critical
          annotations:
            summary: "AutoCache 集群状态异常"
            description: "集群 {{ $labels.cluster }} 状态为 FAIL"
        
        # 内存使用率告警
        - alert: AutoCacheHighMemoryUsage
          expr: |
            autocache_memory_bytes{type="heap_alloc"} / autocache_memory_bytes{type="heap_sys"} > 0.9
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "AutoCache 内存使用率过高"
            description: "内存使用率超过 90%"
        
        # 连接数告警
        - alert: AutoCacheHighConnections
          expr: autocache_connections_total > 1000
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: "AutoCache 连接数过多"
            description: "当前连接数: {{ $value }}"
```

---

## Week 3: 性能测试与论文撰写

### 3.1 性能测试脚本

#### test/benchmark/full_benchmark.sh
```bash
#!/bin/bash

# AutoCache 完整性能测试套件

set -e

HOST=${1:-127.0.0.1}
PORT=${2:-6379}
OUTPUT_DIR="./benchmark_results_$(date +%Y%m%d_%H%M%S)"

mkdir -p $OUTPUT_DIR

echo "=========================================="
echo "AutoCache 性能测试"
echo "=========================================="
echo "目标: $HOST:$PORT"
echo "结果目录: $OUTPUT_DIR"
echo "=========================================="

# 1. 基础性能测试
echo ""
echo "=== 1. 基础性能测试 ==="
redis-benchmark -h $HOST -p $PORT -n 1000000 -c 50 -t set,get,incr,lpush,rpush,lpop,rpop,sadd,hset \
  --csv > $OUTPUT_DIR/basic_benchmark.csv

# 2. 不同数据大小测试
echo ""
echo "=== 2. 数据大小影响测试 ==="
for size in 10 100 1000 10000; do
    echo "Testing with $size bytes..."
    redis-benchmark -h $HOST -p $PORT -n 100000 -c 50 -t set,get -d $size \
      --csv > $OUTPUT_DIR/size_${size}_benchmark.csv
done

# 3. 不同并发数测试
echo ""
echo "=== 3. 并发数影响测试 ==="
for clients in 1 10 50 100 200 500; do
    echo "Testing with $clients clients..."
    redis-benchmark -h $HOST -p $PORT -n 100000 -c $clients -t set,get \
      --csv > $OUTPUT_DIR/clients_${clients}_benchmark.csv
done

# 4. Pipeline 测试
echo ""
echo "=== 4. Pipeline 测试 ==="
for pipeline in 1 10 50 100; do
    echo "Testing with pipeline $pipeline..."
    redis-benchmark -h $HOST -p $PORT -n 100000 -c 50 -t set,get -P $pipeline \
      --csv > $OUTPUT_DIR/pipeline_${pipeline}_benchmark.csv
done

# 5. 延迟分布测试
echo ""
echo "=== 5. 延迟分布测试 ==="
redis-benchmark -h $HOST -p $PORT -n 10000 -c 1 --latency > $OUTPUT_DIR/latency.txt 2>&1 &
LATENCY_PID=$!
sleep 30
kill $LATENCY_PID 2>/dev/null || true

# 6. 长时间稳定性测试
echo ""
echo "=== 6. 稳定性测试 (5分钟) ==="
timeout 300 redis-benchmark -h $HOST -p $PORT -n 10000000 -c 50 -t set,get -l \
  --csv > $OUTPUT_DIR/stability.csv 2>&1 || true

# 7. 生成报告
echo ""
echo "=== 生成测试报告 ==="
cat > $OUTPUT_DIR/report.md << EOF
# AutoCache 性能测试报告

测试时间: $(date)
测试目标: $HOST:$PORT

## 测试结果摘要

### 基础性能
\`\`\`
$(head -20 $OUTPUT_DIR/basic_benchmark.csv)
\`\`\`

### 结论
详细数据请查看各 CSV 文件。
EOF

echo ""
echo "=========================================="
echo "测试完成！"
echo "结果保存在: $OUTPUT_DIR"
echo "=========================================="
```

### 3.2 论文数据收集脚本

#### test/benchmark/collect_paper_data.sh
```bash
#!/bin/bash

# 收集论文所需的性能数据

OUTPUT="paper_data.md"

echo "# AutoCache 性能数据" > $OUTPUT
echo "" >> $OUTPUT
echo "收集时间: $(date)" >> $OUTPUT
echo "" >> $OUTPUT

# 1. 与 Redis 对比
echo "## 1. 与 Redis 性能对比" >> $OUTPUT
echo "" >> $OUTPUT
echo "| 操作 | Redis QPS | AutoCache QPS | 提升比例 |" >> $OUTPUT
echo "|------|-----------|---------------|----------|" >> $OUTPUT

# 测试 Redis
REDIS_SET=$(redis-benchmark -p 6379 -n 100000 -c 50 -t set -q | grep -oP '\d+\.\d+')
REDIS_GET=$(redis-benchmark -p 6379 -n 100000 -c 50 -t get -q | grep -oP '\d+\.\d+')

# 测试 AutoCache
AC_SET=$(redis-benchmark -p 6380 -n 100000 -c 50 -t set -q | grep -oP '\d+\.\d+')
AC_GET=$(redis-benchmark -p 6380 -n 100000 -c 50 -t get -q | grep -oP '\d+\.\d+')

echo "| SET | $REDIS_SET | $AC_SET | $(echo "scale=2; $AC_SET/$REDIS_SET*100-100" | bc)% |" >> $OUTPUT
echo "| GET | $REDIS_GET | $AC_GET | $(echo "scale=2; $AC_GET/$REDIS_GET*100-100" | bc)% |" >> $OUTPUT

# 2. 冷热分层效果
echo "" >> $OUTPUT
echo "## 2. 冷热分层效果" >> $OUTPUT
echo "" >> $OUTPUT
echo "| 指标 | 纯内存 | 冷热分层 | 变化 |" >> $OUTPUT
echo "|------|--------|----------|------|" >> $OUTPUT
# TODO: 添加实际数据

# 3. 延迟分布
echo "" >> $OUTPUT
echo "## 3. 延迟分布 (P50/P99/P999)" >> $OUTPUT
echo "" >> $OUTPUT
# TODO: 添加延迟数据

echo "" >> $OUTPUT
echo "---" >> $OUTPUT
echo "数据收集完成" >> $OUTPUT

cat $OUTPUT
```

### 3.3 论文大纲

```markdown
# 基于云原生的分布式缓存系统设计与实现

## 摘要
- 研究背景与意义
- 主要工作与贡献
- 关键成果

## 第一章 绪论
1.1 研究背景
1.2 研究现状
1.3 研究内容与目标
1.4 论文结构

## 第二章 相关技术与理论基础
2.1 分布式缓存技术
2.2 云原生技术栈
2.3 Kubernetes Operator 模式
2.4 一致性哈希算法

## 第三章 系统需求分析与总体设计
3.1 需求分析
3.2 系统架构设计
3.3 核心模块设计
3.4 数据流设计

## 第四章 核心模块设计与实现
4.1 KV 存储引擎
4.2 Gossip 集群协议
4.3 K8s Operator
4.4 冷热数据分层（创新点 1）
4.5 调度增强（创新点 2）

## 第五章 系统测试与性能分析
5.1 测试环境
5.2 功能测试
5.3 性能测试
  5.3.1 基准性能
  5.3.2 与 Redis 对比
  5.3.3 冷热分层效果
  5.3.4 调度效果
5.4 结果分析

## 第六章 总结与展望
6.1 工作总结
6.2 创新点
6.3 未来工作

## 参考文献

## 致谢
```

---

## 交付物清单

### 代码交付
- [x] 完整的 Prometheus 指标实现
- [x] Grafana Dashboard JSON
- [x] 告警规则配置
- [x] 性能测试脚本

### 文档交付
- [ ] 用户手册
- [ ] API 文档
- [ ] 部署文档
- [ ] 毕业论文

### 测试数据
- [ ] 基准性能数据
- [ ] 与 Redis 对比数据
- [ ] 冷热分层效果数据
- [ ] 调度效果数据

## 答辩准备

### PPT 大纲
1. 选题背景与意义（2页）
2. 技术方案（3页）
3. 系统架构（2页）
4. 核心实现（5页）
5. 创新点（2页）
6. 测试结果（3页）
7. 总结与展望（1页）

### 演示准备
1. 集群创建演示：`kubectl apply -f autocache.yaml`
2. redis-cli 连接演示
3. redis-benchmark 性能演示
4. Grafana 监控演示
5. 扩缩容演示

### 常见问题准备
1. 为什么选择 Golang？
2. 与 Redis 的主要区别？
3. Gossip 协议如何保证一致性？
4. 冷热分层的算法原理？
5. 如何处理脑裂问题？
6. 未来的优化方向？

---

## 时间安排

| 日期 | 任务 |
|------|------|
| 05.19-05.25 | Prometheus 指标 + Grafana |
| 05.26-06.01 | 性能测试 + 数据收集 |
| 06.02-06.08 | 论文撰写 |
| 06.09-06.12 | 论文修改 + 答辩准备 |

祝答辩顺利！🎓
