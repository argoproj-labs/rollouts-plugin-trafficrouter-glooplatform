FROM golang:1.23 as build

WORKDIR /src

ENV CGO_ENABLED=0

COPY go.* ./

RUN go mod download

COPY . ./

ARG GOOS

ARG GOARCH

ARG GOOS

RUN  --mount=type=cache,target=/root/.cache/go-build GOARCH=${GOARCH} GOOS=${GOOS} go build -o /src/main

FROM quay.io/argoproj/argo-rollouts:v1.5.1

COPY  --from=build /src/main /home/argo-rollouts/glooplatform-api-plugin

ENTRYPOINT ["/bin/rollouts-controller"]
