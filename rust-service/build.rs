fn main() -> Result<(), Box<dyn std::error::Error>> {
    tonic_build::configure().build_server(true).compile(
        &["proto/verify.proto", "proto/triton/grpc_service.proto"],
        &["proto", "proto/triton"],
    )?;
    println!("cargo:rerun-if-changed=proto/verify.proto");
    println!("cargo:rerun-if-changed=proto/triton/grpc_service.proto");
    println!("cargo:rerun-if-changed=proto/triton/health.proto");
    println!("cargo:rerun-if-changed=proto/triton/model_config.proto");
    Ok(())
}
