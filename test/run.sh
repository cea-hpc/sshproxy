#!/bin/bash

# adapted from: https://hharnisc.github.io/2016/06/19/integration-testing-with-docker-compose.html

# define some colors to use for output
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

# kill and remove any running containers
cleanup () {
    docker compose -p ci kill
    docker compose -p ci rm -f
}

# catch unexpected failures, do cleanup and output an error message
trap 'cleanup ; printf "${RED}Tests Failed For Unexpected Reasons${NC}\n"' HUP INT QUIT PIPE TERM

# build and run the composed services
docker compose -p ci build && docker compose -p ci up -d
if (( $? != 0 )); then
    printf "${RED}Docker Compose Failed${NC}\n"
    exit -1
fi

# name of tester container
TEST_IMAGE="tester"

# wait for the test service to complete and grab the exit code
TEST_EXIT_CODE=$(docker wait ${TEST_IMAGE})

# output the logs for the test (for clarity)
docker logs ${TEST_IMAGE}

# inspect the output of the test and display respective message
if [[ -z "${TEST_EXIT_CODE}" || "${TEST_EXIT_CODE}" -ne 0 ]]; then
    printf "${RED}Tests Failed${NC} - Exit Code: ${TEST_EXIT_CODE}\n"
    [[ -z "${TEST_EXIT_CODE}" ]] && TEST_EXIT_CODE=2
else
    printf "${GREEN}Tests Passed${NC}\n"
fi

# call the cleanup function
cleanup

# exit the script with the same code as the test service code
exit ${TEST_EXIT_CODE}
