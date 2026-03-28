# Dockerfile for running interop tests in a reproducible container
# Uses Go image as base, installs Deno, mkcert and mage so that the
# existing mage targets can be executed inside the image.

FROM golang:1.26

ARG DENO_VERSION=2.5.0

# install utilities required by mkcert and for downloading tooling
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       libnss3-tools \
       curl \
       unzip \
    && rm -rf /var/lib/apt/lists/*

# install mkcert (Go-based) so run_secure.ts can query CAROOT
RUN go install filippo.io/mkcert@latest

# install Deno (pinned binary from GitHub releases for reproducibility)
RUN curl -fsSL "https://github.com/denoland/deno/releases/download/v${DENO_VERSION}/deno-x86_64-unknown-linux-gnu.zip" -o /tmp/deno.zip \
    && unzip /tmp/deno.zip -d /usr/local/bin \
    && chmod +x /usr/local/bin/deno \
    && rm -f /tmp/deno.zip

# install mage so we can invoke existing targets from inside container
RUN go install github.com/magefile/mage@latest

# create a dedicated non-root user/group and home directory
RUN groupadd -r appuser \
    && useradd -r -g appuser -d /home/appuser -s /usr/sbin/nologin appuser \
    && mkdir -p /home/appuser \
    && chown appuser:appuser /home/appuser

# set HOME so that any tooling (deno, mkcert, etc.) can use a sane path
ENV HOME=/home/appuser

# run Go as unprivileged user without writing to root-owned /go
ENV GOPATH=/home/appuser/go
ENV GOMODCACHE=/home/appuser/go/pkg/mod
ENV GOCACHE=/home/appuser/.cache/go-build

# ensure Go bin and deno binary are on PATH
ENV PATH="/go/bin:/home/appuser/go/bin:/usr/local/bin:${PATH}"

# copy workspace contents (assumes build invoked from repo root)
WORKDIR /work
COPY . /work

# make sure the non-root user owns the workspace
RUN chown -R appuser:appuser /work

# pre-cache TypeScript dependencies for interop client so tests work offline
RUN deno cache moq-web/cli/interop/main.ts

# generate mkcert CA and server certs inside image so wrapper can find them
# any failure producing certs should stop the build
RUN mkcert -install && \
    mkdir -p /root/.local/share/mkcert && \
    cd /work/cmd/interop/server && \
    mkcert -cert-file localhost.pem -key-file localhost-key-file localhost 127.0.0.1 ::1 && \
    # ensure the generated certs and mkcert state are readable by the unprivileged user
    chown -R appuser:appuser /root/.local/share/mkcert /work/cmd/interop/server/*.pem

# tooling cache directories can be created as root during image build; hand them over
RUN chown -R appuser:appuser /home/appuser

# switch to unprivileged user for subsequent operations
USER appuser

# default to a shell; mage targets will be invoked explicitly
CMD ["bash"]
