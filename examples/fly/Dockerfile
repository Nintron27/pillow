ARG GO_VERSION=1
FROM golang:${GO_VERSION}-bookworm as builder

WORKDIR /usr/src/app

# Copy the entire project directory into the container so it has the go.work
COPY ../../ ./

WORKDIR /usr/src/app/examples/fly

# COPY go.mod go.sum ./
RUN go mod download && go mod verify
# COPY . .
RUN go build -v -o /run-app .


FROM debian:bookworm

COPY --from=builder /run-app /usr/local/bin/
CMD ["run-app"]
