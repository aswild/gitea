#!/bin/bash

POSTGRES_USER=giteaIntegration
POSTGRES_PASSWORD=giteaIntegrationPassword
POSTGRES_DB=giteaIntegration

container_opts=(
    -e POSTGRES_USER="$POSTGRES_USER"
    -e POSTGRES_PASSWORD="$POSTGRES_PASSWORD"
    -e POSTGRES_DB="$POSTGRES_DB"
)

die() {
    echo "$*" >&2
    exit 1
}

stop_container() {
    echo "Stopping postgres"
    docker container stop $container_id
}

echo "Starting postgres"
container_id="$(docker run -d --rm "${container_opts[@]}" postgres:12)"
[[ -n $container_id ]] || die "failed to create postgres container"
trap stop_container EXIT

container_ip="$(docker container inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' $container_id)"
[[ -n $container_ip ]] || die "failed to get postgres container ip"

echo "Waiting for postgres to be ready"
while ! nc -z $container_ip 5432; do
    sleep 1
done

test_opts=(
    TEST_PGSQL_HOST="$container_ip"
    TEST_PGSQL_DBNAME="$POSTGRES_DB"
    TEST_PGSQL_USERNAME="$POSTGRES_USER"
    TEST_PGSQL_PASSWORD="$POSTGRES_PASSWORD"
    TEST_PGSQL_SCHEMA="public"
)

echo make "${test_opts[@]}" test-pgsql
make "${test_opts[@]}" test-pgsql
