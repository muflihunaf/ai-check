use std::time::Duration;

use serde::{Deserialize, Serialize};
use thiserror::Error;
use tokio::time::sleep;

#[derive(Debug, Error)]
pub enum TritonError {
    #[error("failed to contact Triton endpoint: {0}")]
    Transport(String),
}

#[derive(Clone)]
pub struct TritonClient {
    endpoint: String,
}

impl TritonClient {
    pub fn new(endpoint: impl Into<String>) -> Self {
        Self {
            endpoint: endpoint.into(),
        }
    }

    pub async fn infer(&self, tensor: &[f32]) -> Result<f32, TritonError> {
        // Simulate gRPC call latency to Triton.
        sleep(Duration::from_millis(50)).await;

        if tensor.is_empty() {
            return Err(TritonError::Transport("tensor cannot be empty".into()));
        }

        let avg_intensity: f32 =
            tensor.iter().map(|value| value.abs()).sum::<f32>() / tensor.len() as f32;
        let score = avg_intensity.min(1.0);

        let payload = TritonPayload {
            endpoint: self.endpoint.clone(),
            average: avg_intensity,
        };
        let _serialized = serde_json::to_string(&payload)
            .map_err(|err| TritonError::Transport(err.to_string()))?;

        Ok(score)
    }
}

#[derive(Serialize, Deserialize)]
struct TritonPayload {
    endpoint: String,
    average: f32,
}
