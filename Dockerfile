ARG GO_IMAGE=golang:1.24-alpine@sha256:8bee1901f1e530bfb4a7850aa7a479d17ae3a18beb6e09064ed54cfd245b7191
ARG RUNTIME_IMAGE=gcr.io/distroless/static:nonroot@sha256:f7f8f729987ad0fdf6b05eeeae94b26e6a0f613bdf46feea7fc40f7bd72953e6

FROM ${GO_IMAGE} AS build

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/yardmaster ./cmd/yardmaster
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/kubectl-yardmaster ./cmd/kubectl-yardmaster
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/yardmaster-dashboard ./cmd/yardmaster-dashboard

FROM ${RUNTIME_IMAGE}

WORKDIR /
COPY --from=build /out/yardmaster /yardmaster
COPY --from=build /out/kubectl-yardmaster /kubectl-yardmaster
COPY --from=build /out/yardmaster-dashboard /yardmaster-dashboard
COPY assets/ /assets/
USER 65532:65532

ENTRYPOINT ["/yardmaster"]
