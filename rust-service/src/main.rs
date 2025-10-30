use std::net::SocketAddr;

use tonic::{transport::Server, Request, Response, Status};
use tracing::{error, info};

use rust_service::{image, triton_client::TritonClient, verify};

use verify::image_processor_server::{ImageProcessor, ImageProcessorServer};
use verify::{VerifyRequest, VerifyResponse};

struct ImageProcessorService {
    triton: TritonClient,
}

#[tonic::async_trait]
impl ImageProcessor for ImageProcessorService {
    async fn process_image(
        &self,
        request: Request<VerifyRequest>,
    ) -> Result<Response<VerifyResponse>, Status> {
        let request = request.into_inner();
        if request.image_data.is_empty() {
            return Err(Status::invalid_argument("image data cannot be empty"));
        }
        if request.user_id.is_empty() {
            return Err(Status::invalid_argument("user_id is required"));
        }

        let tensor = image::preprocess(&request.image_data)
            .map_err(|err| Status::internal(format!("image preprocessing failed: {err}")))?;

        let scores = self
            .triton
            .infer(&tensor)
            .await
            .map_err(|err| Status::internal(format!("triton inference failed: {err}")))?;

        let score = scores.first().copied().unwrap_or_default();
        let success = score >= 0.5;
        let response = VerifyResponse {
            success,
            score,
            message: if success {
                "Verification succeeded".to_string()
            } else {
                "Verification failed".to_string()
            },
        };

        Ok(Response::new(response))
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter("info")
        .with_target(false)
        .init();

    let addr: SocketAddr = "0.0.0.0:50051".parse()?;
    let triton_endpoint =
        std::env::var("TRITON_ENDPOINT").unwrap_or_else(|_| "http://triton:8001".to_string());
    let triton_model =
        std::env::var("TRITON_MODEL_NAME").unwrap_or_else(|_| "face_verification".to_string());
    let triton_input = std::env::var("TRITON_INPUT_NAME").unwrap_or_else(|_| "input".to_string());
    let triton_output =
        std::env::var("TRITON_OUTPUT_NAME").unwrap_or_else(|_| "embedding".to_string());
    let triton_use_tls = std::env::var("TRITON_USE_TLS")
        .map(|value| matches!(value.as_str(), "1" | "true" | "TRUE" | "True"))
        .unwrap_or(false);
    let triton_ca_cert = std::env::var("TRITON_CA_CERT_PATH").ok();

    let service = ImageProcessorService {
        triton: TritonClient::new(
            triton_endpoint,
            triton_model,
            triton_input,
            triton_output,
            triton_use_tls,
            triton_ca_cert,
        ),
    };

    info!(%addr, "Starting Rust image processor");

    if let Err(err) = Server::builder()
        .add_service(ImageProcessorServer::new(service))
        .serve(addr)
        .await
    {
        error!("server error: {err}");
    }

    Ok(())
}
