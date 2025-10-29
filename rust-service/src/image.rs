use image::{imageops::FilterType, DynamicImage};
use ndarray::Array1;
use thiserror::Error;

#[derive(Debug, Error)]
pub enum ImageError {
    #[error("image decoding failed: {0}")]
    Decode(#[from] image::ImageError),
}

pub fn preprocess(bytes: &[u8]) -> Result<Vec<f32>, ImageError> {
    let img = image::load_from_memory(bytes)?;
    let resized = resize_image(&img);
    let rgb = resized.to_rgb8();

    let mut tensor = Vec::with_capacity(224 * 224 * 3);
    for pixel in rgb.pixels() {
        for channel in pixel.0.iter() {
            tensor.push((*channel as f32) / 255.0);
        }
    }
    let array = Array1::from_vec(tensor);
    Ok(array.to_vec())
}

fn resize_image(image: &DynamicImage) -> DynamicImage {
    image.resize_exact(224, 224, FilterType::CatmullRom)
}
