#!/bin/sh

docker build --rm=true -t tudorg/xgo-base base/ && \
    docker build --rm=true -t tudorg/xgo-1.5.1 go-1.5.1/ &&
    docker build --rm=true -t tudorg/beats-builder beats-builder
