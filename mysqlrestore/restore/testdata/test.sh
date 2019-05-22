#!/usr/bin/env bash

set -ux
set -o pipefail

function compose(){
    # shellcheck disable=SC2068
    docker-compose -p "${PROJECT}" -f "${COMPOSE_FILE}" $@
}

function are_containers_healthy(){
    local exitCode
    local container="${PROJECT}_healthcheck_1"
    if ! exitCode=$(docker wait "${container}"); then
        return 1
    fi
    return "${exitCode}"
}

function setup(){
    compose down --volumes --remove-orphans
    compose up -d
    if ! are_containers_healthy; then
        echo "Containers are not healthy"
        compose ps
        compose logs
        return 1
    fi
}

PROJECT=mysqlbackup
RESTORE_CONTAINER="mysql_restore"
BACKUP_CONTAINER="mysql_backup"
# shellcheck disable=SC2046
SCRIPTDIR="$(cd $(dirname "${BASH_SOURCE[0]}") && pwd -P)"
COMPOSE_FILE=${SCRIPTDIR}/docker-compose.yml
TEST_BINARY=${SCRIPTDIR}/restore.test
DOCKER_TEST_BINARY=/usr/local/bin/test/restore.test

GOOS=linux go test -c -o "${TEST_BINARY}" || exit 1
setup || exit 1
compose exec -u root "${BACKUP_CONTAINER}" "${DOCKER_TEST_BINARY}" -test.v -test.run TestBackup || exit 1
compose exec -u root "${RESTORE_CONTAINER}" "${DOCKER_TEST_BINARY}" -test.v -test.run TestRestore || exit 1
rm -f "${TEST_BINARY}"
