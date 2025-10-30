use std::{collections::HashMap, net::SocketAddr, pin::Pin, time::Duration};

use tonic::codegen::tokio_stream::Stream;
use rust_service::{
    triton_client::{
        inference::{
            self,
            grpc_inference_service_server::{GrpcInferenceService, GrpcInferenceServiceServer},
            model_infer_response, InferTensorContents, ModelInferRequest, ModelInferResponse,
        },
        TritonClient,
    },
    ImageTensor,
};
use tokio::{sync::oneshot, time};
use tonic::{async_trait, transport::Server, Request, Response, Status};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn infer_request_serializes_expected_tensor() {
    let addr: SocketAddr = "127.0.0.1:50070".parse().unwrap();
    let model_name = "test-model".to_string();
    let input_name = "input".to_string();
    let output_name = "embedding".to_string();
    let expected_shape = vec![1, 3, 2, 1];

    let mock_service = MockTriton::new(
        model_name.clone(),
        input_name.clone(),
        output_name.clone(),
        expected_shape.clone(),
    );

    let (shutdown_tx, shutdown_rx) = oneshot::channel();

    let server = tokio::spawn(async move {
        Server::builder()
            .add_service(GrpcInferenceServiceServer::new(mock_service))
            .serve_with_shutdown(addr, async {
                let _ = shutdown_rx.await;
            })
            .await
            .unwrap();
    });

    time::sleep(Duration::from_millis(50)).await;

    let client = TritonClient::new(
        format!("http://{}", addr),
        model_name,
        input_name,
        output_name,
        false,
        None,
    );

    let tensor = ImageTensor {
        shape: expected_shape,
        data: vec![0.1, 0.2, 0.3, 0.4, 0.5, 0.6],
    };

    let scores = client.infer(&tensor).await.unwrap();
    assert_eq!(scores, vec![0.25, 0.75]);

    shutdown_tx.send(()).unwrap();
    server.await.unwrap();
}

#[derive(Clone)]
struct MockTriton {
    model_name: String,
    input_name: String,
    output_name: String,
    expected_shape: Vec<i64>,
}

impl MockTriton {
    fn new(
        model_name: String,
        input_name: String,
        output_name: String,
        expected_shape: Vec<i64>,
    ) -> Self {
        Self {
            model_name,
            input_name,
            output_name,
            expected_shape,
        }
    }
}

type MockStream =
    Pin<Box<dyn Stream<Item = Result<inference::ModelStreamInferResponse, Status>> + Send>>;

#[async_trait]
impl GrpcInferenceService for MockTriton {
    type ModelStreamInferStream = MockStream;

    async fn server_live(
        &self,
        _request: Request<inference::ServerLiveRequest>,
    ) -> Result<Response<inference::ServerLiveResponse>, Status> {
        Err(Status::unimplemented("server_live"))
    }

    async fn server_ready(
        &self,
        _request: Request<inference::ServerReadyRequest>,
    ) -> Result<Response<inference::ServerReadyResponse>, Status> {
        Err(Status::unimplemented("server_ready"))
    }

    async fn model_ready(
        &self,
        _request: Request<inference::ModelReadyRequest>,
    ) -> Result<Response<inference::ModelReadyResponse>, Status> {
        Err(Status::unimplemented("model_ready"))
    }

    async fn server_metadata(
        &self,
        _request: Request<inference::ServerMetadataRequest>,
    ) -> Result<Response<inference::ServerMetadataResponse>, Status> {
        Err(Status::unimplemented("server_metadata"))
    }

    async fn model_metadata(
        &self,
        _request: Request<inference::ModelMetadataRequest>,
    ) -> Result<Response<inference::ModelMetadataResponse>, Status> {
        Err(Status::unimplemented("model_metadata"))
    }

    async fn model_infer(
        &self,
        request: Request<ModelInferRequest>,
    ) -> Result<Response<ModelInferResponse>, Status> {
        let request = request.into_inner();
        if request.model_name != self.model_name {
            return Err(Status::invalid_argument("unexpected model name"));
        }
        let input = request
            .inputs
            .into_iter()
            .next()
            .ok_or_else(|| Status::invalid_argument("missing input tensor"))?;
        if input.name != self.input_name {
            return Err(Status::invalid_argument("unexpected input name"));
        }
        if input.shape != self.expected_shape {
            return Err(Status::invalid_argument("unexpected input shape"));
        }
        let contents = input
            .contents
            .ok_or_else(|| Status::invalid_argument("missing input contents"))?;
        if contents.fp32_contents.is_empty() {
            return Err(Status::invalid_argument("missing fp32 contents"));
        }

        let response_tensor = model_infer_response::InferOutputTensor {
            name: self.output_name.clone(),
            datatype: "FP32".to_string(),
            shape: vec![2],
            parameters: HashMap::new(),
            contents: Some(InferTensorContents {
                fp32_contents: vec![0.25, 0.75],
                ..Default::default()
            }),
        };

        let response = ModelInferResponse {
            model_name: self.model_name.clone(),
            outputs: vec![response_tensor],
            raw_output_contents: Vec::new(),
            ..Default::default()
        };

        Ok(Response::new(response))
    }

    async fn model_stream_infer(
        &self,
        _request: Request<tonic::Streaming<ModelInferRequest>>,
    ) -> Result<Response<Self::ModelStreamInferStream>, Status> {
        Err(Status::unimplemented("model_stream_infer"))
    }

    async fn model_config(
        &self,
        _request: Request<inference::ModelConfigRequest>,
    ) -> Result<Response<inference::ModelConfigResponse>, Status> {
        Err(Status::unimplemented("model_config"))
    }

    async fn model_statistics(
        &self,
        _request: Request<inference::ModelStatisticsRequest>,
    ) -> Result<Response<inference::ModelStatisticsResponse>, Status> {
        Err(Status::unimplemented("model_statistics"))
    }

    async fn repository_index(
        &self,
        _request: Request<inference::RepositoryIndexRequest>,
    ) -> Result<Response<inference::RepositoryIndexResponse>, Status> {
        Err(Status::unimplemented("repository_index"))
    }

    async fn repository_model_load(
        &self,
        _request: Request<inference::RepositoryModelLoadRequest>,
    ) -> Result<Response<inference::RepositoryModelLoadResponse>, Status> {
        Err(Status::unimplemented("repository_model_load"))
    }

    async fn repository_model_unload(
        &self,
        _request: Request<inference::RepositoryModelUnloadRequest>,
    ) -> Result<Response<inference::RepositoryModelUnloadResponse>, Status> {
        Err(Status::unimplemented("repository_model_unload"))
    }

    async fn system_shared_memory_status(
        &self,
        _request: Request<inference::SystemSharedMemoryStatusRequest>,
    ) -> Result<Response<inference::SystemSharedMemoryStatusResponse>, Status> {
        Err(Status::unimplemented("system_shared_memory_status"))
    }

    async fn system_shared_memory_register(
        &self,
        _request: Request<inference::SystemSharedMemoryRegisterRequest>,
    ) -> Result<Response<inference::SystemSharedMemoryRegisterResponse>, Status> {
        Err(Status::unimplemented("system_shared_memory_register"))
    }

    async fn system_shared_memory_unregister(
        &self,
        _request: Request<inference::SystemSharedMemoryUnregisterRequest>,
    ) -> Result<Response<inference::SystemSharedMemoryUnregisterResponse>, Status> {
        Err(Status::unimplemented("system_shared_memory_unregister"))
    }

    async fn cuda_shared_memory_status(
        &self,
        _request: Request<inference::CudaSharedMemoryStatusRequest>,
    ) -> Result<Response<inference::CudaSharedMemoryStatusResponse>, Status> {
        Err(Status::unimplemented("cuda_shared_memory_status"))
    }

    async fn cuda_shared_memory_register(
        &self,
        _request: Request<inference::CudaSharedMemoryRegisterRequest>,
    ) -> Result<Response<inference::CudaSharedMemoryRegisterResponse>, Status> {
        Err(Status::unimplemented("cuda_shared_memory_register"))
    }

    async fn cuda_shared_memory_unregister(
        &self,
        _request: Request<inference::CudaSharedMemoryUnregisterRequest>,
    ) -> Result<Response<inference::CudaSharedMemoryUnregisterResponse>, Status> {
        Err(Status::unimplemented("cuda_shared_memory_unregister"))
    }

    async fn trace_setting(
        &self,
        _request: Request<inference::TraceSettingRequest>,
    ) -> Result<Response<inference::TraceSettingResponse>, Status> {
        Err(Status::unimplemented("trace_setting"))
    }

    async fn log_settings(
        &self,
        _request: Request<inference::LogSettingsRequest>,
    ) -> Result<Response<inference::LogSettingsResponse>, Status> {
        Err(Status::unimplemented("log_settings"))
    }
}
