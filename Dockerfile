FROM golang:1.26.8-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -o /out/distsys ./cmd/distsys
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 app
COPY --from=build /out/distsys /usr/local/bin/distsys
USER app
EXPOSE 8083
ENTRYPOINT ["distsys"]
CMD ["serve"]
