#!/bin/bash

set -uex
set -o pipefail

DATE=$(date +%Y%m%d-%H%M%S)
ENVIRONMENT=$(hostname -d | awk -F. '{print $1}')
MYSQL_BACKUP_DIR=/opt/mysql_backups
S3_DIR=${MYSQL_BACKUP_DIR}/s3_backup
BACKUP_DIR=${MYSQL_BACKUP_DIR}/backup_dir
FULL_BACKUP_NAME=full_backup_${ENVIRONMENT}_${DATE}
INCREMENTAL_BACKUP_NAME=incremental_backup_${ENVIRONMENT}_${DATE}

function full_backup(){
  xtrabackup --safe-slave-backup --slave-info --backup --target-dir="${BACKUP_DIR}"/"${FULL_BACKUP_NAME}" || exit 1
  print_section "FULL BACKUP COMPLETE"
  gzip_backup "${FULL_BACKUP_NAME}" || exit 1
  return 0
}

function gzip_backup(){
  local name=$1
  cd "${BACKUP_DIR}"

  if [ ! -d ${S3_DIR} ];then
    mkdir ${S3_DIR}
  fi

  print_section "GZIP BACKUP DIR TO ${S3_DIR}"
  tar -zcvf "${name}".gz "${BACKUP_DIR}"/"${name}"/ || exit 1
  mv "${BACKUP_DIR}"/"${name}".gz "${S3_DIR}" || exit 1
  return 0
}

function incremental_backup(){
  local PREVIOUS_BACKUP=$1
  xtrabackup --safe-slave-backup --slave-info --backup --target-dir="$BACKUP_DIR"/"${INCREMENTAL_BACKUP_NAME}" --incremental-basedir="${PREVIOUS_BACKUP}" --no-timestamp || exit 1
  print_section "INCREMENTAL BACKUP COMPLETE"
  gzip_backup "${INCREMENTAL_BACKUP_NAME}" || exit 1
  return 0
}

function prepare_backup(){
  local backup_dir=$1
  local full_backup_dir=$(find "${backup_dir}" -type d -name "full_backup*")
  local incremental_backup_list=$(find "${backup_dir}" -type d -name "incremental_backup*")
  local incremental_backup_array=("$incremental_backup_list")

  xtrabackup --prepare --apply-log-only "${full_backup_dir}" || exit 1

  if [[ ! -z ${incremental_backup_array[*]} ]]; then
    for x in ${incremental_backup_array[*]}; do
      if [[ $x != ${incremental_backup_array[-1]} ]]; then
        xtrabackup --prepare --apply-log-only "${full_backup_dir}" --incremental-dir=${x} || exit 1
      else
        xtrabackup --prepare "${full_backup_dir}" --incremental-dir=${x} || exit 1
      fi
    done
  fi

  print_section "DATABASE BACKUPS PREPARE COMPLETED"
  return 0
}

function print_section(){
  local delimeter="--------------------------"
  local message=$1
  cat << EOF
  echo ${delimeter}
  echo ${message}
  echo ${delimeter}
EOF
}

function restore_backup(){
  local backup_dir=$1
  local full_backup_dir=$(find "${backup_dir}" -type d -name "full_backup*")

  if [[ -d "${full_backup_dir}" ]]; then
    systemctl stop mysqld
    rm -rf /var/lib/mysql/data
    xtrabackup --copy-back "${full_backup_dir}" || exit 1
  else
    print_section "A FULL BACKUP DOES NOT EXIST"
    exit 1
  fi

  touch /var/lib/mysql/logs/db-bin.index
  chown -R mysql:mysql /var/lib/mysql
  systemctl start mysqld

  print_section "MYSQL RESTORE COMPLETE"

  return 0
}

function usage() {

    cat <<EOF
USAGE: $0 [OPTION ... ]
Run script to backup or restore the database. Default directory is /opt/mysql_backups.
If a full backups exists and is less than 7 days old, an incremental backup will create.
Otherwise a full backup will be created.
All backups will be gzipped and backuped up to an S3 bucket.
Options:
-h         --help           Display the usage message and exit
-b         --backup         Backup the database, will be either a full or incremental backup depending on what is needed.
-r         --restore        Restore database from backups
EOF

}

while :; do
  [[ $# -lt 1 ]] && break
  case "$1" in
    -h| --help) usage; exit 0;;
    -b| --backup) ARG=$1; shift;;
    -r| --restore) ARG=$1; shift;;
    *) echo "Unsupported option: ${1}"; usage; exit 1;;
    esac
    shift
done

if [[ -z "${ARG:-}" ]]; then
    echo "ERROR: No ARG line option set!"
    usage
    exit 1
fi

if [ "${ARG}" == "-b" ] ||  [ "${ARG}" == "--backup" ]; then

    # If no backup found, do a full backup and exit
    # If backup is older than 6 days, delete and create a new full backup and exit
    # If backup exists and is less than 7 days old, pass to incremental backup
    BACKUP_DIRECTORY=$(find "${BACKUP_DIR}" -type d -name "full_backup*")
    if  [[ ! -d "${BACKUP_DIRECTORY}" ]]; then
        print_section "BACKUP DOES NOT EXIST, CREATING A FULL BACKUP"
        full_backup || exit 1

        # INCREMENTAL BACKUP IS NOT NEEDED, EXITING
        exit 0
    fi

    # Check if it is older than 6 days, if so delete and create a new full backup
    FULL_BACKUP_DATE_CHECK=$(find "${BACKUP_DIR}" -iname "full_backup*" -atime +6 -type d)
    if [ "${FULL_BACKUP_DATE_CHECK}" ]; then
        print_section "BACKUP IS OLDER THAN 6 DAYS OLD, REMOVING ALL BACKUPS AND CREATING NEW ONE"
        rm -rf "${MYSQL_BACKUP_DIR}"/*
        full_backup || exit 1

        # INCREMENTAL BACKUP IS NOT NEEDED, EXITING
        exit 0
    fi

    PREVIOUS_BACKUP=$(find "${BACKUP_DIR}" -iname '*_backup*' -atime -1 -type d |sort -nr | head -1)
    if [[ ! -d "${PREVIOUS_BACKUP}" ]]; then
        print_section "A PREVIOUS DAYS BACKUP DOES NOT EXIST, CANNOT CONTINUE WITH INCREMENTAL BACKUPS"
        exit 1
    else
        incremental_backup "${PREVIOUS_BACKUP}" || exit 1
        exit 0
    fi
fi

if [ "${ARG}" == "-r" ] ||  [ "${ARG}" == "--restore" ]; then
    print_section "PREPARING BACKUP"
    prepare_backup "${MYSQL_BACKUP_DIR}" || exit 1

    print_section "RESTORING BACKUP"
    restore_backup "${MYSQL_BACKUP_DIR}" || exit 1
fi

return 0

