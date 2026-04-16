Perfect! Here's a hands-on tutorial for the forecaster:

3-Minute Quick Start

Step 1: Seed data (30 seconds)

 cd forecaster
 uv run python scripts/seed_test_data.py --clear-existing --minutes 1800

✅ Creates realistic 30 hours of metrics for all 3 services (5400 rows total)

Step 2: Start server (20 seconds)

 uv run forecaster

✅ Listen for "grpc server listening" → ready on port 50051

Step 3: Train a model (in another terminal)

 cd forecaster
 python - <<'PY'
 import grpc, sys
 sys.path.insert(0, '../gen/python')
 from optipilot.v1 import prediction_pb2, prediction_pb2_grpc
 
 channel = grpc.insecure_channel('localhost:50051')
 client = prediction_pb2_grpc.OptiPilotServiceStub(channel)
 resp = client.TriggerRetrain(
     prediction_pb2.TriggerRetrainRequest(service_name='api-gateway'),
     timeout=180,
 )
 print(resp)
 PY

✅ Trains on 30 hours of data, returns MAPE accuracy and promotion decision

Step 4: Inspect results

 sqlite3 forecaster_registry.db "SELECT version, is_promoted, round(validation_mape,4) FROM models WHERE 
service_name='api-gateway' ORDER BY id;"

✅ See model versions, promotion status, and accuracy

--------------------------------------------------------------------------------------------------------------------------

Key Concepts

┌────────────────────────┬────────────────────────────────────────────────────────────────────────────────────────────────┐
│ Term                   │ Meaning                                                                                        │
├────────────────────────┼────────────────────────────────────────────────────────────────────────────────────────────────┤
│ MAPE                   │ Mean Absolute Percentage Error (2% = very accurate, 30% = acceptable baseline)                 │
├────────────────────────┼────────────────────────────────────────────────────────────────────────────────────────────────┤
│ Promotion              │ New model replaces old one when MAPE < threshold AND new_MAPE ≤ old_MAPE * (1 + tolerance)     │
├────────────────────────┼────────────────────────────────────────────────────────────────────────────────────────────────┤
│ p50 / p90              │ Two models: p50 = median forecast, p90 = pessimistic upper bound                               │
├────────────────────────┼────────────────────────────────────────────────────────────────────────────────────────────────┤
│ Quantile               │ LightGBM mode that predicts at specific percentiles (not just mean)                            │
├────────────────────────┼────────────────────────────────────────────────────────────────────────────────────────────────┤
│ Feature engineering    │ 19 features: RPS lags (1-30min), rolling stats, temporal, resource metrics                     │
└────────────────────────┴────────────────────────────────────────────────────────────────────────────────────────────────┘

--------------------------------------------------------------------------------------------------------------------------

Common Commands

Reset everything:

 cd forecaster && rm -rf models *.db *.db-*

Train all 3 services:

 for svc in api-gateway order-service payment-service; do
   python - <<PY
 import grpc, sys
 sys.path.insert(0, '../gen/python')
 from optipilot.v1 import prediction_pb2, prediction_pb2_grpc
 channel = grpc.insecure_channel('localhost:50051')
 client = prediction_pb2_grpc.OptiPilotServiceStub(channel)
 resp = client.TriggerRetrain(prediction_pb2.TriggerRetrainRequest(service_name='$svc'), timeout=180)
 print(f"$svc: {resp.success}")
 PY
 done

Check database:

 # All model versions
 sqlite3 forecaster_registry.db "SELECT service_name, version, is_promoted, round(validation_mape,4) FROM models ORDER BY 
service_name, id;"
 
 # Metric counts per service
 sqlite3 forecaster_metrics.db "SELECT service_name, COUNT(*) as rows FROM metrics GROUP BY service_name;"

--------------------------------------------------------------------------------------------------------------------------
