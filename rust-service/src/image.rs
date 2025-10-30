use image::{imageops::FilterType, DynamicImage, RgbImage};
use thiserror::Error;

#[derive(Debug, Clone)]
pub struct ImageTensor {
    pub shape: Vec<i64>,
    pub data: Vec<f32>,
}

#[derive(Debug, Error)]
pub enum ImageError {
    #[error("image decoding failed: {0}")]
    Decode(#[from] image::ImageError),
}

pub fn preprocess(bytes: &[u8]) -> Result<ImageTensor, ImageError> {
    let img = image::load_from_memory(bytes)?;
    let resized = resize_image(&img);
    let rgb = resized.to_rgb8();

    let data = to_chw_tensor(&rgb);

    Ok(ImageTensor {
        shape: vec![1, 3, 224, 224],
        data,
    })
}

fn resize_image(image: &DynamicImage) -> DynamicImage {
    image.resize_exact(224, 224, FilterType::CatmullRom)
}

fn to_chw_tensor(image: &RgbImage) -> Vec<f32> {
    let mut tensor = Vec::with_capacity((image.width() * image.height() * 3) as usize);

    for channel in 0..3 {
        for y in 0..image.height() {
            for x in 0..image.width() {
                let pixel = image.get_pixel(x, y);
                tensor.push(pixel[channel] as f32 / 255.0);
            }
        }
    }

    tensor
}
