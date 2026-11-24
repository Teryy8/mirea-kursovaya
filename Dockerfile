FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY web/ ./web/
RUN mkdir -p /out/data \
    && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/livetraffic .

FROM alpine:3.23
COPY --from=build --chown=10001:10001 /out/data/ /data/
COPY --from=build /out/livetraffic /usr/local/bin/livetraffic
USER 10001:10001
ENV DB_PATH=/data/analytics.db HTTP_ADDR=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/livetraffic"]
CMD ["server"]
