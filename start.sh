#! /usr/bin/env bash

echo "Loading settings from .env"
set -a && source ${1:-.env} && go fmt && go build && ./xmpp-webhook
