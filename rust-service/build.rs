fn main() -> Result<(), Box<dyn std::error::Error>> {
    tonic_build::configure()
        .build_server(true)
        .compile(&["proto/verify.proto"], &["proto"])?;
    println!("cargo:rerun-if-changed=proto/verify.proto");
    Ok(())
}
