# Build the TypeScript client
FROM node:20-alpine AS web
WORKDIR /src
RUN npm install --global typescript@5
COPY tsconfig.json ./
COPY web ./web
RUN tsc

# Build the Go server
FROM golang:1.22-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -o /tank-ops .

# Small runtime image
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /tank-ops /tank-ops
COPY --from=web /src/web/dist /web/dist
ENV ADDR=0.0.0.0:8000 STATIC_DIR=/web/dist
EXPOSE 8000
ENTRYPOINT ["/tank-ops"]
