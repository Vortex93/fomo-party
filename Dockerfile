# Override GO_IMAGE with a reviewed/pinned maintained builder image for releases.
ARG GO_IMAGE=golang:alpine
FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
COPY web ./web
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /family-party ./cmd/server

FROM scratch
COPY --from=build /family-party /family-party
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/family-party"]
