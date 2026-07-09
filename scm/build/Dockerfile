# Set defaults for USERNAME, UID, GID
ARG USERNAME=danm
ARG UID=147
ARG GID=147

#
# Stage: Build container. Used to build all binaries, once
#
FROM golang:1.13-alpine3.10 AS builder
MAINTAINER Levente Kale <levente.kale@gmail.com>

ARG USERNAME
ARG UID
ARG GID
ARG COMMIT_HASH
ARG LATEST_TAG

RUN apk add --no-cache ca-certificates make git bash

RUN mkdir -p ${GOPATH}/src/github.com/danm-cni/danm
COPY / ${GOPATH}/src/github.com/danm-cni/danm/

WORKDIR ${GOPATH}/src/github.com/danm-cni/danm

RUN scm/build/build.sh \
 && adduser -u ${UID} -D -H -s /sbin/nologin ${USERNAME} \
 && chown root:${USERNAME} /go/bin/netwatcher /go/bin/svcwatcher /go/bin/webhook \
 && chmod 0750 /go/bin/*


#
# (Intermediate) Stage: Alping base image. Includes common user account and environment
#                       for netwatcher, svcwatcher, webhook
#
FROM alpine:3.11 AS base-alpine
ARG USERNAME
ARG UID
ARG GID

RUN adduser -u ${UID} -D -H -s /sbin/nologin ${USERNAME}
WORKDIR /
USER ${USER}

#
# Stage: Netwatcher
#
FROM base-alpine AS netwatcher
MAINTAINER Levente Kale <levente.kale@gmail.com>

COPY --from=builder /go/bin/netwatcher /usr/local/bin/netwatcher
RUN apk add --no-cache --virtual .tools libcap  \
 && setcap cap_sys_ptrace,cap_sys_admin,cap_net_admin=eip /usr/local/bin/netwatcher \
 && apk del .tools
ENTRYPOINT ["/usr/local/bin/netwatcher"]


#
# Stage: Svcwatcher
#
FROM base-alpine AS svcwatcher
MAINTAINER Levente Kale <levente.kale@gmail.com>

COPY --from=builder /go/bin/svcwatcher /usr/local/bin/svcwatcher
ENTRYPOINT ["/usr/local/bin/svcwatcher"]


#
# Stage: Webhook
#
FROM base-alpine AS webhook
MAINTAINER Levente Kale <levente.kale@gmail.com>

COPY --from=builder /go/bin/webhook /usr/local/bin/webhook
ENTRYPOINT ["/usr/local/bin/webhook"]


#
# Stage: CNI plugins daemonset
#
# Note that unlike the other containers, this needs to run as root as
# it places CNI plugins into the host's filesystem.
#
FROM alpine:3.11 AS danm-cni-plugins

VOLUME ["/host/cni"]

RUN mkdir /cni
COPY --from=builder /go/bin/danm /go/bin/fakeipam /cni/

COPY scm/build/cni_ds/entrypoint.sh /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
