#!/usr/bin/env bash

set -ue
set -o pipefail

S3_DOWNLOAD_DIR=/opt/mysql_backups
SNAPSHOT_BACKUP_DIR=/opt/mysql_backups/incrementals
MYSQL_DATA_DIR=/var/lib/mysql/data
MYSQL_LOG_DIR=/var/lib/mysql/logs
S3_BUCKET_NAME=global-backup-storage-bucket-flhspka03aso

function print_section(){
  local delimeter="---------------------"
  local message=$1
  cat << EOF
  "${delimeter}"
  "${message}"
EOF
}

function list_snapshots(){
  local environment=$1
  local snapshot_list=$(mktemp /tmp/snapshots.XXX)
  local unique_snapshots=$(mktemp /tmp/snapshots.XXX)

  #shellcheck disable=SC2005,SC2046,SC2086,SC2027
  echo $(aws s3api list-objects --bucket "${S3_BUCKET_NAME}" --prefix "mysqlbackups/"${environment}"" --query 'Contents[].Key' | jq -r '.[]? | select(. | contains(".tar.gz")) | select(. | contains("snapshot"))'| sort -u) >> "${snapshot_list}"

  echo -e "Here are the snapshots for bucket ${S3_BUCKET_NAME} in ${environment} \n"
  #shellcheck disable=SC2002,SC2013
  for snapshot in $(cat "${snapshot_list}" | sort -nu);do
    basename "$(dirname "${snapshot}")" >> "${unique_snapshots}";
  done
  #shellcheck disable=SC2002
  cat "${unique_snapshots}" | sort -u
  echo -e
  echo "Select a snapshot to restore from and pass in to restore: mysql-restore.sh -o restore -e prod -s snapshot_2019-04-02"

  rm -rf "${snapshot_list}" "${unique_snapshots}"
  return 0
}

function decompress_backups(){
  time find "${MYSQL_DATA_DIR}" -name "*.qp" -type f -exec du --null -h '{}' + | sort -rhz | sed -z 's/[[:digit:]\.]\+[A-Za-z]\+[[:space:]]\+//' | parallel -j 50% --null 'qpress -do {} > {.} && rm -f {}'
  time find "${S3_DOWNLOAD_DIR}" -name "*.qp" -type f -exec du --null -h '{}' + | sort -rhz | sed -z 's/[[:digit:]\.]\+[A-Za-z]\+[[:space:]]\+//' | parallel -j 50% --null 'qpress -do {} > {.} && rm -f {}'
  return 0
}

function download_snapshot(){

  local snapshot=$1

  if [[ ! -d "${S3_DOWNLOAD_DIR}" ]]; then
    mkdir "${S3_DOWNLOAD_DIR}" || exit 1
  else
    rm -rf "${S3_DOWNLOAD_DIR}"/*
  fi

  # Keeping extracted tar files separatedl from the s3 backup dir because the s3 directory structure when downloading files.
  if [[ ! -d "${SNAPSHOT_BACKUP_DIR}" ]]; then
    mkdir -p "${SNAPSHOT_BACKUP_DIR}" || exit 1
  fi

  #shellcheck disable=SC2027,SC2086,SC2016,SC2140
  echo "Executing: aws s3 cp s3://"${S3_BUCKET_NAME}"/mysqlbackups/"${ENV}"/ "${S3_DOWNLOAD_DIR}" --recursive --exclude "*" --include '*/"${snapshot}"/*.gz'"
  #shellcheck disable=SC2016,SC2140
  aws s3 cp s3://"${S3_BUCKET_NAME}"/mysqlbackups/"${ENV}"/ "${S3_DOWNLOAD_DIR}" --recursive --exclude "*" --include '*/"${snapshot}"/*.gz'

  pushd ${S3_DOWNLOAD_DIR}
  #shellcheck disable=SC2044,SC2086
  for tar_file in $(find . -mindepth 1 -name '*.gz');do
    if [[ $(basename "${tar_file}" | cut -d. -f1) =~ ^full* ]]; then
      tar -zxf "${tar_file}" --strip-components=1 --directory="${MYSQL_DATA_DIR}" | parallel -j 50% --null {}
    else
      tar -zxf "${tar_file}" --directory="${SNAPSHOT_BACKUP_DIR}" | parallel -j 50% --null {}
    fi
  done

  find . -mindepth 1 -name '*.gz'  -exec rm -f {} \;

  return 0
}

function mysql_checks(){

  if (pgrep mysql);then
    echo "MYSQLD IS STILL RUNNING!"
    if [[ $(mysql -s -r -e "SELECT @@global.read_only;" | awk '{ print $1 }') == 0 ]];then
        echo "THIS IS ALSO THE MASTER DATABASE!"
        exit 1
    fi
    exit 1
  fi

  if [[ "$(ls -A $MYSQL_DATA_DIR)" ]]; then
    echo "${MYSQL_DATA_DIR} is not empty! Cannot proceed"
    exit 1
  fi

  if [[ "$(ls -A $MYSQL_LOG_DIR)" ]]; then
    echo "${MYSQL_LOG_DIR} is not empty! Cannot proceed"
    exit 1
  fi

  if [[ ! -d "${MYSQL_DATA_DIR}" ]];then
    echo "${MYSQL_DATA_DIR} does not exist, creating..."
    mkdir "${MYSQL_DATA_DIR}"
  fi

  if [[ ! -d "${MYSQL_LOG_DIR}" ]];then
    mkdir "${MYSQL_LOG_DIR}"
  fi

  return 0
}

function prepare_backups(){

  # Create a bash array of the incremental backups from S3_BACKUP_DIR(/opt/mysql_backups/backups)
  local incremental_dir_array=()
  while IFS=  read -r -d $'\0'; do
      incremental_dir_array+=("$REPLY")
  done < <(find "${SNAPSHOT_BACKUP_DIR}" -mindepth 1 -maxdepth 1 -type d -print0)

  # prepare the backups, starting w/ the full backup.
  # Check incremental_dir_array if there are any incremental backups
  # If there are no incremental backups, prepare the full backup and exit.
  # If there are incremental backups, prepare the full and iterate through incremental_dir_array and daisy chain full and incremental backups.
  if ${#incremental_dir_array[@]} == 0;then
    innobackupex --apply-log --parallel=8 --use-memory=4G "${MYSQL_DATA_DIR}"|| exit 1
    return 0
  else
    innobackupex --apply-log --redo-only --parallel=8 --use-memory=4G "${MYSQL_DATA_DIR}" || exit 1
  fi

  for x in "${incremental_dir_array}"; do
    if [[ $x != "${incremental_dir_array[${#incremental_dir_array[@]}-1]}" ]]; then
      innobackupex --apply-log --redo-only --parallel=8 --use-memory=4G "${MYSQL_DATA_DIR}" --incremental-dir=$x
    else
      innobackupex --apply-log --parallel=8 --use-memory=4G "${MYSQL_DATA_DIR}" --incremental-dir=$x
    fi
  done

  return 0
}

function start_mysql(){

  chown -R mysql:mysql /var/lib/mysql

  systemctl enable mysqld
  systemctl start mysqld

  return 0
}

function usage() {

    cat <<EOF
USAGE: $0 [OPTION ... ]
This script is used to provide the lasted snapshots(both full and incremental backups) and to restore mysql to the snapshot provided.

Options:
-h         --help          Display the usage message and exit

-o         --operation     Operation to perform.  list or restore.
                           list - to show a list of snapshots.  For example: mysql-restore.sh -o list -e qa
                           restore - to restore the snapshot provided.  For example: mysql-restore -o restore -e qa -s snapshot_2019_04_02

-e         --environment   The environment needs to be provided.(qa, ga, or prod)
                           Example: mysql-restore.sh -o list -e qa
                           Example: mysql-restore -o restore -e qa -s snapshot_2019_04_02

-r         --restore       Restore database from s3 bucket, use the output provided from the list option above
                           Example: mysql-restore.sh -r prod snapshot_2019-04-02
EOF

}

if [[ "$#" -lt 1 ]]; then
    echo "ERROR: Command line arguments are not set!"
    usage
    exit 1
fi

OPERATION=""
ENV=""
SNAPSHOT=""
while :; do
  [[ $# -lt 1 ]] && break
  case "$1" in
    -h| --help) usage; exit 0;;
    -o| --operation) OPERATION=$2;;
    -e| --environment) ENV=$2;;
    -s| --snapshot) SNAPSHOT=$2;;
    *) echo "Unsupported option: ${1}"; usage; exit 1;;
    esac
    shift 2
done

if [[ "${OPERATION}" != "list" && "${OPERATION}" != "restore" ]]; then
   echo "invalid operation specified. operation must be set with --operation and valid values are list and restore"
   exit 1
fi

if [[ "${OPERATION}" == "list" ]]; then
  if [[ -z "${ENV}" ]];then
    echo "Must specify and environment qa, ga, or prod with the -e flag"
    echo "mysql-restore.sh -o list -e qa"
    echo -e
    usage
    exit 1
  fi
  list_snapshots "${ENV}" || exit 1
  exit 0
fi

if [[ "${OPERATION}" == "restore"  ]]; then

  if [[ -z "${SNAPSHOT}" ]];then
    echo "Must provide the snapshot date you want to restore from!"
    echo "mysql-restore.sh -o restore -e qa -s snapshot_2019_04_02"
    echo -e
    echo "Check the s3 bucket for a list of available backups: mysql-restore.sh -o list -e qa"
    exit 1
  fi

  print_section "Check mysql process and directories exist"
  mysql_checks

  echo "downloading snapshot ${SNAPSHOT}"
  download_snapshot "${SNAPSHOT}"

  echo "decompressing snapshots"
  decompress_backups

  echo "preparing snapshots"
  prepare_backups

  echo "starting mysql"
  start_mysql

  echo "restore complete"

  exit 0

fi