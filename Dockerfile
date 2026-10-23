# build
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/nuitelle .

# runtime
FROM alpine:3.22
RUN apk add --no-cache ca-certificates ffmpeg tzdata \
 && adduser -D -H -u 1000 nuitelle
COPY --from=build /out/nuitelle /usr/local/bin/nuitelle
USER nuitelle
VOLUME ["/data", "/media"]
EXPOSE 8770
ENV VELA_ROOTS=/media VELA_DB=/data/nuitelle.db VELA_COVERS=/data/covers VELA_ADDR=0.0.0.0:8770
ENTRYPOINT ["nuitelle"]
CMD ["serve"]
