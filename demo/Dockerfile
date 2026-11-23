# syntax=docker/dockerfile:1
#
# hazel demo image: the hazel CLI, sqlite3 and a sample schema.
# Build from the repo root:
#   docker build -f demo/Dockerfile -t hazel-demo .
# Run:
#   docker run --rm -it hazel-demo
#
FROM golang:1.27.4-alpine AS build
WORKDIR /src
COPY go.* /src/
RUN go mod download
COPY . /src/
ARG HAZEL_VERSION=devel
# modernc.org/sqlite is pure Go, so no C compiler is needed.
RUN CGO_ENABLED=0 go build -o /out/hazel -ldflags "-X main.version=${HAZEL_VERSION#v}" ./cmd/hazel

FROM alpine:3.24
RUN apk add --no-cache bash vim less bat sqlite && \
    adduser -D -h /home/demo -s /bin/bash demo && \
    printf "alias ll='ls -lF'\nalias vi='vim'\nexport LESSOPEN='| bat --color=always -pp %%s'\nexport LESS='-R'\n" > /home/demo/.bashrc && \
    printf "syntax on\n" > /home/demo/.vimrc && \
    chown demo:demo /home/demo/.bashrc /home/demo/.vimrc

COPY --from=build /out/hazel /usr/local/bin/hazel
COPY demo/entrypoint.sh /entrypoint.sh
COPY demo/manual.txt demo/current.sql demo/desired.sql /demo/

# Load current.sql at build time, so that every `docker run --rm` starts
# from the same state.
RUN chmod +x /entrypoint.sh && \
    sqlite3 -bail /demo/demo.db < /demo/current.sql && \
    chown -R demo:demo /demo

ENV HAZEL_PAGER="bat -l sql -p --paging=never --color=always"

WORKDIR /demo
USER demo

# Source revision this image was built from (set in CI).
ARG HAZEL_REVISION
ENV HAZEL_REVISION=${HAZEL_REVISION}

ENTRYPOINT ["/entrypoint.sh"]
CMD ["bash"]
