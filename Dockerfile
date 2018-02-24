
###################################
#Build stage
FROM golang:1.10-alpine3.7 AS build-env

ARG GITEA_VERSION
ARG TAGS="sqlite"
ENV TAGS "bindata $TAGS"

#Build deps
RUN apk --no-cache add build-base git

#Setup repo
COPY . ${GOPATH}/src/code.gitea.io/gitea
WORKDIR ${GOPATH}/src/code.gitea.io/gitea

#Checkout version if set
RUN if [ -n "${GITEA_VERSION}" ]; then git checkout "${GITEA_VERSION}"; fi \
 && make clean generate build

FROM alpine:3.7
LABEL maintainer="maintainers@gitea.io"

EXPOSE 22

RUN apk --no-cache add \
    bash \
    ca-certificates \
    curl \
    gettext \
    git \
    openssh \
    s6 \
    sqlite \
    su-exec \
    tzdata

RUN addgroup \
    -S -g 995 \
    git && \
  adduser \
    -S -H -D \
    -h /data/gitea/git \
    -s /bin/bash \
    -u 995 \
    -G git \
    git && \
  echo "git:$(dd if=/dev/urandom bs=24 count=1 status=none 2>/dev/null | base64)" | chpasswd

ENV USER git
ENV GITEA_CUSTOM /data/gitea

VOLUME ["/data"]

ENTRYPOINT ["/usr/bin/entrypoint"]
CMD ["/bin/s6-svscan", "/etc/s6"]

COPY docker /
COPY --from=build-env /go/src/code.gitea.io/gitea/gitea /app/gitea/gitea
