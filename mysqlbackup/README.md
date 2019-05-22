# mysqlbackup

## What is this used for?
Runs as a service and attempts to crete a full backup every 24 hours and an incremental backup every 60 minutes.

mysqlbackup will

  - Attempt an incremental backup every 60 minutes.
    - If a backup exists, an incremental is taken
    - If the full backup is older than 24 hours, a new full backup is taken.
    - Any incremental backups thereafter are using the new full backup as it's base.
  - mysqlbackup will Tar + Gzip the database contents
  - Upload the tar.gz to an S3 bucket
  - Delete the tar.gz file after it has successfully uploaded to S3
  - The full and incremental backups will remain on the server in /opt/mysql_backups/db_backups
  - mysqlbackup uses innodbackupex - which is part of percona-xtrabackup-24 version: 2.4.13
    - '--slave-info' and '--safe-slave-backup' are turned on in case this is a slave datababase.
    - mysqlbackup will also compress the backup, so you WILL need to decompress prior to restoring the database.
      - See wiki 
      [Restoring Backups](https://team.gohealth.net/confluence/display/DEVOPS/Restore+Procedure+from+S3+Backups) 
      for details on how to decompress and restore backups.
  - New Relic integration and monitoring - ToDo
   
Only 1 type of backup will occur any given time.  So if a full backup takes 6 hours, the incremental will be taken 
after the full backup completes.

## Why is this necessary?
Automated database backups are a good thing to have in case our master and slave databases become corrupt/unusable.

## How to run.
You will need the following environmental variables set prior to running mysqlbackup, otherwise the AWS uploader will 
throw and error.
```
MYSQL_PASSWORD
AWS_ACCESS_KEY_ID
AWS_SECRET_ACCESS_KEY
* Note if a my.cnf file exists in the users home directory, this will be used a a default if mysql_username and password
are not set.
```

These command line arguements need to be set when running mysqlbackup
backupdir              Default: /opt/mysql_backups
incremental_interval   Default: 60 minutes
aws_region             Default: us-ease-2
env
bucket_name
mysql_user             Default: root
debug                  Default: false - used to change log levels to debug
```
mysqlbackup -bucket_name data-bucket-name -env TESTING -incremental_interval 1m -backupdir /opt/mysql_backups

or 

mysqlbackup -bucket_name data-bucket-name -env TESTING -incremental_interval 1m -debug true
```

Directory structure based on the default backupdir:
BASE_DIR = "/opt/mysql_backups"
BACKUP_DIR = "/opt/mysql_backups/db_backups"
S3_DIR     = "/opt/mysql_backups/s3_backups"

mysqlbackup will create the other necessary directories, you MUST make sure enough diskspace is allocated for whatever
directory you use.


You can install the rpm and execute the service.  Puppet configurations and secrets will load the necessary variables
in /etc/systemd/system/mysqlbackup.service.d/override.conf
* You must make this PR separately on they system this will run on.




