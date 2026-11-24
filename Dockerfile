FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY web/ ./web/
RUN CGO_ENABLED=0 go build -trimpath -o /out/livetraffic .

FROM alpine:3.23
RUN addgroup -g 10001 app \
    && adduser -D -H -u 10001 -G app app \
    && mkdir /data \
    && chown app:app /data
COPY --from=build /out/livetraffic /usr/local/bin/livetraffic
USER 10001:10001
ENV DB_PATH=/data/analytics.db HTTP_ADDR=0.0.0.0:8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/livetraffic"]
CMD ["server"]
