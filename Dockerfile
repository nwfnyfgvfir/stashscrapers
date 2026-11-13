FROM --platform=$BUILDPLATFORM condaforge/miniforge3:latest AS build
WORKDIR /src
COPY environment.yml ./
RUN conda env create -f environment.yml && conda clean -afy
COPY go.mod go.sum ./
RUN conda run -n stashscrapers go mod download
COPY . .
RUN conda run -n stashscrapers python tools/check.py && \
    conda run -n stashscrapers python tools/package_source.py
ARG TARGETOS
ARG TARGETARCH
RUN conda run -n stashscrapers env CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags="-s -w" -o /out/stashscrapers ./cmd/stashscrapers && \
    mkdir -p /out/data

FROM condaforge/miniforge3:latest AS python-runtime
RUN conda create -n scripts -c conda-forge python=3.12 pip -y && conda clean -afy && \
    mkdir -p /data && chown -R 10001:10001 /data
COPY --from=build /out/stashscrapers /usr/local/bin/stashscrapers
COPY LICENSE /usr/share/licenses/stashscrapers/LICENSE
USER 10001:10001
ENV STASHSCRAPERS_DATA=/data STASHSCRAPERS_LISTEN=0.0.0.0:8080
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/stashscrapers"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 CMD ["/usr/local/bin/stashscrapers", "--healthcheck"]

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/stashscrapers /stashscrapers
COPY --from=build --chown=65532:65532 /out/data /data
COPY LICENSE /LICENSE
ENV STASHSCRAPERS_DATA=/data STASHSCRAPERS_LISTEN=0.0.0.0:8080
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/stashscrapers"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 CMD ["/stashscrapers", "--healthcheck"]
