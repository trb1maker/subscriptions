FROM golang:1.27.0 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG PACKAGE=./services/gateway/cmd/gateway
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/service ${PACKAGE}

FROM scratch

COPY --from=build /out/service /service
ENTRYPOINT ["/service"]
