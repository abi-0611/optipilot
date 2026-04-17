# Changelog

All notable changes to this project are documented in this file.

## v0.1.0

Initial public project release.

### Added

- Contract-first gRPC API (`prediction.proto`) with generated Go/Python stubs.
- Go controller loops for service discovery, Prometheus collection, prediction flow integration, and decision auditing.
- Python forecaster service with metrics ingestion, model registry, LightGBM quantile training, inference, recalibration, and drift detection.
- Controller dashboard backend:
  - REST API for services, history, audit, controls, and health
  - WebSocket event stream for realtime metrics/predictions/decisions/alerts
- Runtime safety controls:
  - global kill switch
  - per-service pause
  - mode overrides (`shadow`, `recommend`, `autonomous`)
  - recommendation approval/rejection endpoints
- Helm chart (`charts/optipilot`) for cluster deployment with configurable values and schema validation.
- Vegeta load-test scenarios and targets in `loadtest/`.
- Demo walkthrough documentation in `docs/DEMO.md`.
- Project documentation set (`README`, architecture/API/operations/ML/contributing docs).

### Notes

- Frontend API wiring is still being aligned with latest backend surfaces.
- Some forecaster responses remain data-maturity dependent until enough service history is available.

