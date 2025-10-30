pub mod image;
pub mod triton_client;

pub use image::ImageTensor;

pub mod verify {
    tonic::include_proto!("verify");
}
