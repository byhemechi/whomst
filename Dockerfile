FROM --platform=$BUILDPLATFORM golang:1.26-alpine3.23 AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY main.go .
ARG TARGETOS TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /bin/httpdebug

FROM scratch
COPY --from=build /bin/httpdebug /bin/httpdebug
CMD [ "/bin/httpdebug" ]
