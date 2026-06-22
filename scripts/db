#!/usr/bin/env bash

set -euo pipefail

function init_db() {
    if [ ! -d "$PGDATA" ]; then
        echo "Initializing PostgreSQL database..."
        initdb -D "$PGDATA" --auth=trust --no-instructions

        # Configure PostgreSQL
        echo "unix_socket_directories = '$PGDATA'" >> "$PGDATA/postgresql.conf"

        # Start PostgreSQL
        start_db

        # Create development database
        createdb "$PGDATABASE"
        echo "Database '$PGDATABASE' created successfully"
    else
        echo "PostgreSQL data directory already exists at $PGDATA"
    fi
}

function start_db() {
    init_db
    if pg_ctl status -D "$PGDATA" > /dev/null 2>&1; then
        echo "PostgreSQL is already running"
    else
        echo "Starting PostgreSQL..."
        pg_ctl start -D "$PGDATA" -l "$PGDATA/postgresql.log"
        echo "PostgreSQL started successfully"
    fi
}

function stop_db() {
    if pg_ctl status -D "$PGDATA" > /dev/null 2>&1; then
        echo "Stopping PostgreSQL..."
        pg_ctl stop -D "$PGDATA"
        echo "PostgreSQL stopped successfully"
    else
        echo "PostgreSQL is not running"
    fi
}

function status_db() {
    if pg_ctl status -D "$PGDATA" > /dev/null 2>&1; then
        echo "PostgreSQL is running"
        echo "Database: $PGDATABASE"
        echo "Port: $PGPORT"
        echo "Data directory: $PGDATA"
    else
        echo "PostgreSQL is not running"
    fi
}

# Check if required environment variables are set
if [ -z "${PGDATA:-}" ] || [ -z "${PGDATABASE:-}" ] || [ -z "${PGPORT:-}" ]; then
    echo "Error: Required environment variables are not set"
    echo "Please make sure PGDATA, PGDATABASE, and PGPORT are set in your .envrc"
    exit 1
fi

# Command processing
case "${1:-}" in
    "init")
        init_db
        ;;
    "start")
        start_db
        ;;
    "stop")
        stop_db
        ;;
    "status")
        status_db
        ;;
    *)
        echo "Usage: $0 {init|start|stop|status}"
        exit 1
        ;;
esac
