● Bash(rm -f optipilot.db optipilot.db-wal optipilot.db-shm /tmp/oppid /tmp/oplog)
  ⎿  Done

● Everything works as expected:

  1. Builds cleanly — go build . with zero errors, no new external deps
  2. Startup — logs discovery mode, scaling mode, prometheus address, collector interval, 3 static services
  3. Collector loop — immediately attempts collection for all 3 services (api-gateway, order-service,
  payment-service), queries Prometheus concurrently, logs per-metric WARN for each connection-refused
  (expected with no Prometheus running), reports summary "services":3,"success":0,"failed":3
  4. No panics — graceful degradation when Prometheus is unreachable
  5. Clean shutdown — SIGTERM produces "shutting down" and exits cleanly

  Files created:
  - internal/discovery/discovery.go — ServiceDiscovery interface, DiscoveryEvent types
  - internal/discovery/static.go — StaticDiscovery converting config entries to ServiceTargets
  - internal/discovery/kubernetes.go — stub with detailed TODO for client-go informer implementation
  - internal/collector/collector.go — Prometheus HTTP client, concurrent per-service queries, batch persist,
  in-memory cache

  File updated:
  - main.go — wires discovery + collector into the startup/shutdown lifecycle
