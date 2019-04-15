#!/usr/bin/env bash
set -u
set -o pipefail

function print_section (){
    local text=$1
    local delimiter="==================================="
    cat << EOF
${delimiter}
${text}
${delimiter}
EOF
}

function cleanup_failed_backups(){
  echo "ERROR - failed to back up couchbase from node ${CB_HOST}. Environment: ${COUCHBASE_ENV}, Cluster: ${COUCHBASE_CLUSTER}"
  print_section "removing ${CB_BACKUP_ROOT_DIR}"
  rm -rf "${CB_BACKUP_ROOT_DIR}"
  exit 1
}

if [[ $# -lt 2 ]]; then
    echo "ERROR - missing arguments: environment cluster"
    exit 1
fi

readonly COUCHBASE_ENV=$1
readonly COUCHBASE_CLUSTER=$2

<% if @facts['app_environment'] == 'prod' -%>
if [[ "${COUCHBASE_ENV}" == "prod" && "${COUCHBASE_CLUSTER}" == "cache" ]];then
    BACKUPUSERNAME=<%= scope.function_dig_hiera(['backups.prod.couchbase.username']) %>
    BACKUPPASSWORD=<%= scope.function_dig_hiera(['backups.prod.couchbase.password']) %>
    CB_HOST=cache1.bo-prod.norvax.net # These are the actual node server names - it's not an IP address
elif [[ "${COUCHBASE_ENV}" == "prod" && "${COUCHBASE_CLUSTER}" == "cb" ]]; then
    BACKUPUSERNAME=<%= scope.function_dig_hiera(['backups.prod.couchbase.username']) %>
    BACKUPPASSWORD=<%= scope.function_dig_hiera(['backups.prod.couchbase.password']) %>
    CB_HOST=10.238.9.172
<% else -%>
if [[ "${COUCHBASE_ENV}" == "qa" && "${COUCHBASE_CLUSTER}" == "cache" ]];then
    BACKUPUSERNAME=<%= scope.function_dig_hiera(['backups.qa.couchbase.username']) %>
    BACKUPPASSWORD=<%= scope.function_dig_hiera(['backups.qa.couchbase.password']) %>
    CB_HOST=cache1.qa.norvax.net # Upgraded hosts are using host names instead of IPs now
elif [[ "${COUCHBASE_ENV}" == "qa" && "${COUCHBASE_CLUSTER}" == "cb" ]];then
    BACKUPUSERNAME=<%= scope.function_dig_hiera(['backups.qa.couchbase.username']) %>
    BACKUPPASSWORD=<%= scope.function_dig_hiera(['backups.qa.couchbase.password']) %>
    CB_HOST=192.168.40.217
elif [[ "${COUCHBASE_ENV}" == "uat" && "${COUCHBASE_CLUSTER}" == "cache" ]];then
    BACKUPUSERNAME=<%= scope.function_dig_hiera(['backups.uat.couchbase.username']) %>
    BACKUPPASSWORD=<%= scope.function_dig_hiera(['backups.uat.couchbase.password']) %>
    CB_HOST=cache1.uat.norvax.net
elif [[ "${COUCHBASE_ENV}" == "uat" && "${COUCHBASE_CLUSTER}" == "cb" ]]; then
    BACKUPUSERNAME=<%= scope.function_dig_hiera(['backups.uat.couchbase.username']) %>
    BACKUPPASSWORD=<%= scope.function_dig_hiera(['backups.uat.couchbase.password']) %>
    CB_HOST=192.168.46.122
# TODO Add support for GA
<% end -%>
else
    echo "ERROR - couchbase environment or cluster is not valid"
    exit 1
fi

export AWS_ACCESS_KEY_ID=<%= scope.function_dig_hiera(['backups.aws_s3.aws_access_key']) %>
export AWS_SECRET_ACCESS_KEY=<%= scope.function_dig_hiera(['backups.aws_s3.aws_secret_access_key']) %>
BUCKET_NAME=<%= scope.function_dig_hiera(['backups.aws_s3.bucketName']) %>

DATE=$(date +%Y%m%d-%H%M%S)
EXPIRATION_DATE=$(date -d "$(date)+3 month" +%Y%m%d-%H%M%S)
CBBACKUP="/opt/couchbase/bin/cbbackup"

if [[ ! -s "${CBBACKUP}" ]]; then
    echo "ERROR - file does not exist ${CBBACKUP}"
    exit 1
fi

CB_BACKUP_ROOT_DIR="/opt/backups/couchbase/${COUCHBASE_CLUSTER}/${COUCHBASE_ENV}"
DAILY_BACKUP_DIR="${CB_BACKUP_ROOT_DIR}/${DATE}"

[[ ! -d "${DAILY_BACKUP_DIR}" ]] && mkdir -p "${DAILY_BACKUP_DIR}" || exit 1

CB_ADDRESS="couchbase://${CB_HOST}:8091"
print_section "Executing /opt/couchbase/bin/cbbackup for couchbase backup in ${DAILY_BACKUP_DIR} from ${CB_ADDRESS}"
${CBBACKUP} "${CB_ADDRESS}" "${DAILY_BACKUP_DIR}" -u "${BACKUPUSERNAME}" -p "${BACKUPPASSWORD}" || cleanup_failed_backups

pushd $(dirname "${CB_BACKUP_ROOT_DIR}")

# Dump some information about the backup in a text file that will be in the root of the tarball contents
echo "Generated: $(date) on $(hostname -f)" > backup-info.txt
echo "Cluster: ${COUCHBASE_CLUSTER}" >> backup-info.txt
echo "Cluster address: ${CB_ADDRESS}" >> backup-info.txt
echo "Environment: ${COUCHBASE_ENV}" >> backup-info.txt
echo "Couchbase user: ${BACKUPUSERNAME}" >> backup-info.txt

print_section "Tar couchbase backup"
BACKUP_TAR="couchbase-backup-${COUCHBASE_CLUSTER}-${COUCHBASE_ENV}-${DATE}.tgz"
if ! tar cvfz "${BACKUP_TAR}" "${DAILY_BACKUP_DIR}"; then
    echo "ERROR - failed to tar up couchbase backup. Environment: ${COUCHBASE_ENV}, Cluster: ${COUCHBASE_CLUSTER}"
    exit 1
fi

print_section "Push tar file to s3 bucket"
if ! aws s3 cp "${BACKUP_TAR}" "s3://${BUCKET_NAME}/couchbase/${COUCHBASE_CLUSTER}/${COUCHBASE_ENV}/${BACKUP_TAR}" --storage-class STANDARD_IA --sse --expires "${EXPIRATION_DATE}"; then
   echo "ERROR - failed to copy up couchbase backup to s3. Environment: ${COUCHBASE_ENV}, Cluster: ${COUCHBASE_CLUSTER}"
   exit 1
fi

print_section "Clean up local backup directory"
if ! rm -vrf "${CB_BACKUP_ROOT_DIR}"; then
    echo "ERROR - failed to remove local backup directory. Environment: ${COUCHBASE_ENV}, Cluster: ${COUCHBASE_CLUSTER}"
    exit 1
fi

