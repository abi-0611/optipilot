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