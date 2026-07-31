package client

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// metricChainReachable is 1 when the mining node's most recent chain RPC poll
// succeeded and 0 when it failed (e.g. the chain endpoint is down or refusing
// connections). It is set on every epoch poll in MonitorEpochs, the node's 6s
// chain heartbeat.
//
// Part of the miner_* metric contract consumed by ault-exporter (see that
// repo's AultMinerChainUnreachable alert). It is defined here rather than in
// pkg/mining/stats.go with the other miner_* metrics because the value is
// produced in this package's poll loop, and pkg/client must not import
// pkg/mining (that would invert the dependency and risk an import cycle).
//
// promauto registers to the default registry, which is what api/server.go
// exposes via promhttp.Handler(), so the series appears on /metrics alongside
// the pkg/mining metrics.
var metricChainReachable = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "miner_chain_reachable",
	Help: "1 if the last chain RPC poll succeeded, 0 if it failed (chain unreachable).",
})
