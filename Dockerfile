FROM golang:1.24-alpine AS build

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/yardmaster ./cmd/yardmaster
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/kubectl-yardmaster ./cmd/kubectl-yardmaster
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/yardmaster-dashboard ./cmd/yardmaster-dashboard

FROM gcr.io/distroless/static:nonroot

WORKDIR /
COPY --from=build /out/yardmaster /yardmaster
COPY --from=build /out/kubectl-yardmaster /kubectl-yardmaster
COPY --from=build /out/yardmaster-dashboard /yardmaster-dashboard
COPY assets/ /assets/
USER 65532:65532

ENTRYPOINT ["/yardmaster"]
