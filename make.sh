#!/bin/sh

if [ "$1" = "push-docker-plugin" ]; then
    DOCKER_PLUGIN_TYPE=$2 exec make push-docker-plugin
fi

exec make "$@"
