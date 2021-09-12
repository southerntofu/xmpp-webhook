#! /usr/bin/env bash

function help () {
    echo "./sync.sh [--edit] FILE"
    echo "  Synchronize file with remote \$XMPP_REMOTE via SSH, optionally editing the file before."
    echo "  Only synchronizes the file when go build succeeds, and applies go fmt first."
}

set -a && source .env
if  [ -z ${XMPP_REMOTE+x} ]; then
    echo "\$XMPP_REMOTE unset. Please set it to a value scp can understand in ./.env file."
    exit 1
fi

EDIT="0"

if [ $# -ge 1 ]; then
    case $1 in
        "edit"|"--edit"|"-e")
            EDIT="1"
            ;;
        "help")
            help
            exit 0
            ;;
        *)
            if [ -f ${1} ]; then
                # Editing this file was requested
                SOURCE=$1
                # Since a source was specified, we don't accept more args
                shift $#
            else
                echo "ERROR: Argument not understood or source file not found: $1"
                exit 1
            fi
            ;;
    esac
    # Remove first argument to study the (potential next)
    shift
fi

# If we still have an argument to process, it must be a source file
if [ $# -ge 1 ]; then
    if [ -f ${1} ]; then
        SOURCE=${1}
    else
        echo "ERROR: Source file not found: $1"
        exit 1
    fi
else
    SOURCE="main.go"
fi
    
echo "Syncing $SOURCE to $XMPP_REMOTE"

if [ ${EDIT} -eq "1" ]; then
    vim $SOURCE && go fmt && go build && scp $SOURCE $XMPP_REMOTE
else
    go fmt && go build && scp $SOURCE $XMPP_REMOTE
fi
