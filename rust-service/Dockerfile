FROM rust:1.72 AS builder
WORKDIR /usr/src/app

COPY Cargo.toml Cargo.lock* ./
COPY build.rs ./
COPY src ./src
COPY proto ./proto

RUN cargo build --release

FROM debian:12-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        libopencv-core-dev \
    && rm -rf /var/lib/apt/lists/*
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
WORKDIR /app

COPY --from=builder /usr/src/app/target/release/rust-service ./server

EXPOSE 50051

ENTRYPOINT ["/app/server"]
