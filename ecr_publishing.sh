#!/usr/bin/env bash

# shellcheck disable=SC2155,SC2181

##Script to backup Nexus repositories to ECR and S3 buckets.
#If the Image Repository does not exist on AWS ECR, this script will fail.  You must add it in through terraform

set -o pipefail
set -u

function backup_img(){

    #shellcheck disable=SC2001
    local LOCAL_REPO=$1
    local S3_IMAGE=$(echo /"${LOCAL_REPO}" | sed -e 's/\/.*\///g').tar
    local YEAR=$(date '+%Y')
    local MONTH=$(date '+%b')
    local DAY=$(date '+%d')

    print_section "SAVING ${LOCAL_REPO} AS A TAR FILE TO BE UPLOADED AS ${S3_IMAGE}"
    if ! docker save -o "${S3_IMAGE}" "${NEXUS_REPO}"; then
        echo "ERROR - failed to docker save a local copy of ${LOCAL_REPO}, need this to back up to s3."
        return 1
    fi

    print_section "COPYING THE IMAGE TO S3"
    if ! aws s3 cp "${S3_IMAGE}" s3://"${BUCKET_NAME}"/"${YEAR}"/"${MONTH}"/"${DAY}"/"${IMAGE}_${TAG}" --sse AES256; then
        echo "ERROR - failed to copy the image ${S3_IMAGE} to the S3 bucket ${BUCKET_NAME}"
        rm "${S3_IMAGE}"
        return 1
    fi

    rm "${S3_IMAGE}"

    return 0
}

function ecr_repo_check() {

    local LOCAL_REPO="${1}"
    #shellcheck disable=SC2016
    #Seperate this out, validate it and edit return
    #checks if the repo exists in the ECR
    ECR_CHECK=$(aws ecr describe-repositories --region "${AWS_REGION}" | jq -r --arg LOCAL_REPO "${LOCAL_REPO}" '.repositories[] | select(.repositoryName==$LOCAL_REPO)')
    if [ -z "${ECR_CHECK}" ]; then
        echo "ERROR - Repo ${IMAGE} does not exist in the ECR, please add it in using the clouldformation stack"
        echo "ERROR - Check terrarform repo for ECR Publishing documentation"
        return 1
    fi

    return 0
}

function print_section (){
    local text=$1
    local delimiter="==================================="
    cat << EOF
${delimiter}
${text}
${delimiter}
EOF
}

function push_to_ecr() {

    local LOCAL_REPO="${1}"
    local login_output=$(mktemp)

    if ! aws ecr get-login --region "${AWS_REGION}" --no-include-email > "${login_output}"; then
 	    echo "ERROR - failed to get login script for ECR from aws"
	    return 1
    fi

    chmod 600 "${login_output}"

    if ! bash "${login_output}"; then
        echo "ERROR - failed to login into the Elastic Container Registry"
        rm "${login_output}"
        return 1
    fi

    #shellcheck disable=SC2002
    ECR_REGISTRY_ADDR=$( cat "${login_output}" | awk '{print $7}' | sed 's|https://||g' )

    rm "${login_output}"

    #shellcheck disable=SC2140
    ECR_REPOSITORY="${ECR_REGISTRY_ADDR}"/"${LOCAL_REPO}":"${TAG}"

    echo "${NEXUS_REPO}"
    if ! docker tag "${NEXUS_REPO}" "${ECR_REPOSITORY}"; then
        echo "ERROR - failed to properly tag the repo before publishing to ECR"
        return 1
    fi

    if ! docker push "${ECR_REPOSITORY}"; then
        echo "ERROR - failed to push to ECR"
        return 1
    fi

    return 0
}

function usage() {

    cat <<EOF
USAGE: $0 [OPTION ... ]
Run script to publish docker images to AWS ECR and to an S3 bucket.
Options:
-h         --help           display the usage message and exit
-i         --image          specify the image to backup to and publish to ECR
-t         --tag            must use a tag for the the image w/ a prefix consistent with your repository(i.e v12345)
EOF

}

while :; do
    [[ $# -lt 1 ]] && break
    case "$1" in
      -h|--help) usage;exit 0;;
      -i|--image) readonly IMAGE=$2; shift;;
      -t|--tag) readonly TAG=$2; shift;;
      *) echo "Unsupported option: ${1}"; usage; exit 1;;
    esac
    shift
done

if [[ -z "${IMAGE:-}" ]]; then
    echo "ERROR: --image argument is not set. its required."
    usage
    exit 1
fi

if [[ ! "${IMAGE}" =~ ^gohealth/* ]]; then
    echo "ERROR: failed to parse --image argument because it do not match re pattern, must start with gohealth/<image_name>"
    usage
    exit 1
fi

if [[ -z "${TAG:-}" ]]; then
    echo "ERROR: --tag argument is not set. its required."
    usage
    exit 1
fi

if [[ "${TAG}" == 'latest' ]]; then
    echo "WARN: --tag argument is set to latest. please ensure this is what you want."
fi

NEXUS_REPO=nexus.dev.norvax.net:8082/"${IMAGE}:${TAG}"


AWS_REGION="$(printenv AWS_REGION)"

if [[ "${AWS_REGION}" != "us-east-2" ]]; then
    echo "AWS_REGION is '${AWS_REGION}' when it must be us-east-2. S3 bucket ${BUCKET_NAME} for backing up docker imagese only exists in us-east-2"
    exit 1
fi

# shellcheck disable=SC2016
BUCKET_NAME=$(aws cloudformation describe-stacks --region "${AWS_REGION}" --stack-name global-ecr-image-backup \
		--query 'Stacks[*].Outputs[?OutputKey==`Bucket`].OutputValue' --output text)

if [[ $? -ne 0 || -z "${BUCKET_NAME}" ]];then
	echo "ERROR - failed to get bucket name from output in cloudformation stack global-ecr-image-backup"
	exit 1
fi

print_section "CHECKING AWS CREDENTIALS ARE PROPERLY CONFIGURED"
#shellcheck disable=SC2091
if ! $(printenv AWS_ACCESS_KEY_ID >/dev/null && printenv AWS_SECRET_ACCESS_KEY >/dev/null ); then
    echo "ERROR AWS CREDENTIALS ARE NOT SET, RUN aws configure TO SET THEM. IF THIS IS A BUILD SLAVE, OPEN AN SREREQ TICKET"
    exit 1
fi

print_section "CHECKING IF THE REPO EXISTS IN ECR"
ecr_repo_check "${IMAGE}" || exit 1

print_section "PULLING DOWN THE IMAGE from Nexus Repository"
if ! docker pull "${NEXUS_REPO}"; then
    echo "ERROR - failed to 'docker pull' from the Nexus repo for ${NEXUS_REPO}, please check that the image exists"
    exit 1
fi

if ! backup_img "${IMAGE}"; then
    echo "ERROR - failed to backup ${IMAGE} the repo to the S3 bucket: "
    exit 1
fi

print_section "PUSHING THE IMAGE TO ECR"
if ! push_to_ecr "${IMAGE}"; then
    echo "ERROR - failed to push ${IMAGE} the repo to ECR"
    exit 1
fi

print_section "BACKUP COMPLETE"
exit 0
