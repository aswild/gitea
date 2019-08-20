
###################################
#Build stage
FROM golang:1.12-alpine3.10 AS build-env

#Build deps
RUN apk --no-cache add build-base git

ARG GOPROXY
ENV GOPROXY ${GOPROXY:-direct}

ARG GITEA_VERSION
#ARG TAGS="sqlite sqlite_unlock_notify"
ARG TAGS
ARG VERSION
ENV TAGS "bindata $TAGS"

#Setup repo
COPY . ${GOPATH}/src/code.gitea.io/gitea
WORKDIR ${GOPATH}/src/code.gitea.io/gitea

#Checkout version if set
RUN if [ -n "${GITEA_VERSION}" ]; then git checkout "${GITEA_VERSION}"; fi \
 && make GITEA_VERSION="${VERSION}" clean generate build

FROM alpine:3.10
LABEL maintainer="maintainers@gitea.io"

RUN set -s && \
    apk --no-cache add \
        bash \
        ca-certificates \
        curl \
        gettext \
        git \
        openssh \
        s6 \
        su-exec \
        tzdata \
        && \
    addgroup -S git && \
    adduser -S -H -D \
        -h /data/gitea \
        -s /bin/bash \
        -G git \
        git && \
    echo "git:$(dd if=/dev/urandom bs=24 count=1 status=none 2>/dev/null | base64)" | chpasswd

ENV USER git
ENV GITEA_CUSTOM /data/gitea

VOLUME ["/data/gitea"]

ENTRYPOINT ["/usr/bin/entrypoint"]
CMD ["/bin/s6-svscan", "/etc/s6"]

COPY docker/root /
COPY --from=build-env /go/src/code.gitea.io/gitea/gitea /app/gitea/gitea
RUN set -x && \
    rm -f /Makefile && \
    ln -s /app/gitea/gitea /usr/local/bin/gitea
