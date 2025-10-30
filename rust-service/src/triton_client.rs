use std::{collections::HashMap, sync::Arc, time::Duration};

use byteorder::{ByteOrder, LittleEndian};
use http::Uri;
use thiserror::Error;
use tokio::sync::Mutex;
use tonic::transport::{Certificate, Channel, ClientTlsConfig, Endpoint};

use crate::image::ImageTensor;

pub mod inference {
    tonic::include_proto!("inference");
}

use inference::grpc_inference_service_client::GrpcInferenceServiceClient;
use inference::model_infer_request::{InferInputTensor, InferRequestedOutputTensor};
use inference::{InferParameter, InferTensorContents, ModelInferRequest};

#[derive(Debug, Error)]
pub enum TritonError {
    #[error("failed to contact Triton endpoint: {0}")]
    Transport(String),
    #[error("invalid Triton response: {0}")]
    InvalidResponse(String),
    #[error("invalid Triton configuration: {0}")]
    Configuration(String),
}

#[derive(Clone)]
pub struct TritonClient {
    endpoint: String,
    model_name: String,
    input_name: String,
    output_name: String,
    use_tls: bool,
    ca_certificate_path: Option<String>,
    channel: Arc<Mutex<Option<GrpcInferenceServiceClient<Channel>>>>,
}

impl TritonClient {
    #[allow(clippy::too_many_arguments)]
    pub fn new(
        endpoint: impl Into<String>,
        model_name: impl Into<String>,
        input_name: impl Into<String>,
        output_name: impl Into<String>,
        use_tls: bool,
        ca_certificate_path: Option<String>,
    ) -> Self {
        Self {
            endpoint: endpoint.into(),
            model_name: model_name.into(),
            input_name: input_name.into(),
            output_name: output_name.into(),
            use_tls,
            ca_certificate_path,
            channel: Arc::new(Mutex::new(None)),
        }
    }

    pub async fn infer(&self, tensor: &ImageTensor) -> Result<Vec<f32>, TritonError> {
        if tensor.data.is_empty() {
            return Err(TritonError::InvalidResponse(
                "tensor data cannot be empty".into(),
            ));
        }

        let mut client_guard = self.channel.lock().await;
        if client_guard.is_none() {
            *client_guard = Some(self.connect().await?);
        }
        let client = client_guard.as_mut().expect("client must be initialized");

        let mut inputs = Vec::with_capacity(1);
        inputs.push(self.build_input_tensor(tensor));

        let mut outputs = Vec::with_capacity(1);
        outputs.push(self.build_requested_output());

        let request = ModelInferRequest {
            model_name: self.model_name.clone(),
            model_version: String::new(),
            id: String::new(),
            parameters: HashMap::new(),
            inputs,
            outputs,
            raw_input_contents: Vec::new(),
        };

        let response = client
            .model_infer(request)
            .await
            .map_err(|err| TritonError::Transport(err.to_string()))?
            .into_inner();

        self.extract_scores(response)
    }

    fn build_input_tensor(&self, tensor: &ImageTensor) -> InferInputTensor {
        let contents = InferTensorContents {
            fp32_contents: tensor.data.clone(),
            ..Default::default()
        };

        InferInputTensor {
            name: self.input_name.clone(),
            datatype: "FP32".to_string(),
            shape: tensor.shape.clone(),
            parameters: HashMap::new(),
            contents: Some(contents),
        }
    }

    fn build_requested_output(&self) -> InferRequestedOutputTensor {
        let mut parameters = HashMap::new();
        parameters.insert(
            "binary_data".to_string(),
            InferParameter {
                parameter_choice: Some(inference::infer_parameter::ParameterChoice::BoolParam(
                    false,
                )),
            },
        );

        InferRequestedOutputTensor {
            name: self.output_name.clone(),
            parameters,
        }
    }

    async fn connect(&self) -> Result<GrpcInferenceServiceClient<Channel>, TritonError> {
        let tls_domain = if self.use_tls {
            let uri = self
                .endpoint
                .parse::<Uri>()
                .map_err(|err| TritonError::Configuration(err.to_string()))?;
            Some(
                uri.host()
                    .ok_or_else(|| {
                        TritonError::Configuration(
                            "TLS endpoint must include a host name".to_string(),
                        )
                    })?
                    .to_string(),
            )
        } else {
            None
        };

        let mut endpoint = Endpoint::from_shared(self.endpoint.clone())
            .map_err(|err| TritonError::Configuration(err.to_string()))?
            .connect_timeout(Duration::from_secs(5))
            .timeout(Duration::from_secs(15));

        if self.use_tls {
            let mut tls = ClientTlsConfig::new();
            if let Some(domain) = tls_domain {
                tls = tls.domain_name(domain);
            }
            if let Some(path) = &self.ca_certificate_path {
                let pem = tokio::fs::read(path)
                    .await
                    .map_err(|err| TritonError::Configuration(err.to_string()))?;
                tls = tls.ca_certificate(Certificate::from_pem(pem));
            }
            endpoint = endpoint
                .tls_config(tls)
                .map_err(|err| TritonError::Configuration(err.to_string()))?;
        }

        let channel = endpoint
            .connect()
            .await
            .map_err(|err| TritonError::Transport(err.to_string()))?;

        Ok(GrpcInferenceServiceClient::new(channel))
    }

    fn extract_scores(
        &self,
        response: inference::ModelInferResponse,
    ) -> Result<Vec<f32>, TritonError> {
        let mut scores = if let Some(output) = response
            .outputs
            .iter()
            .find(|output| output.name == self.output_name)
        {
            if let Some(contents) = &output.contents {
                if !contents.fp32_contents.is_empty() {
                    contents.fp32_contents.clone()
                } else {
                    Vec::new()
                }
            } else {
                Vec::new()
            }
        } else {
            return Err(TritonError::InvalidResponse(format!(
                "missing output tensor '{}' in response",
                self.output_name
            )));
        };

        if !scores.is_empty() {
            return Ok(scores);
        }

        if let Some(raw_bytes) = response.raw_output_contents.first() {
            if raw_bytes.len() % std::mem::size_of::<f32>() != 0 {
                return Err(TritonError::InvalidResponse(
                    "output tensor byte length is not a multiple of 4".into(),
                ));
            }
            let element_count = raw_bytes.len() / std::mem::size_of::<f32>();
            scores = (0..element_count)
                .map(|index| {
                    let start = index * 4;
                    LittleEndian::read_f32(&raw_bytes[start..start + 4])
                })
                .collect();
        }

        if scores.is_empty() {
            return Err(TritonError::InvalidResponse(
                "no FP32 data found in Triton response".into(),
            ));
        }

        Ok(scores)
    }
}
