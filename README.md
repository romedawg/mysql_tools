# mysqlrestore

## What is this used for?
Used to list snapshots(full and incremental backups) that are available on s3 
bucket:global-backup-storage-bucket-flhspka03aso/mysqlbackups

You can also restore mysql to a point in time buy providing a snapshot that is available on the previously
noted s3 bucket.

## Why is this necessary?
Restoring MySql can be tedious and error prone.  We are also actively sending mysql backups to s3 so this is a good way to 
restore and verify backups.

## How to run.
You will need the following environmental variables set.
```
AWS_ACCESS_KEY_ID
AWS_SECRET_ACCESS_KEY
AWS_REGION
```

These command line arguments need to be set when running mysqlrestore
ops        = operation to be run.  list or restore
env        = environment to run the operation(qa, uat, prod)
bucket     - snapshots to restore.
snapshot   - snapshot to be restored
restoreDir - restore directory to use for full and incremental backups.  Be sure enough disk space is allocated.
debug 
```
# list the snapshots:
mysqlrestore -operation list -env testing -bucket global-backup-storage-bucket-flhspka03aso

# restore mysql to snapshot mysqlbackups/qa/2019/April/9/snapshot_2019_04_09
mysqlrestore -operation restore -env qa -bucket global-backup-storage-bucket-flhspka03aso -snapshot mysqlbackups/qa/2019/April/9/snapshot_2019_04_09 -directory /opt/mysql_backups/s3 -debug true
```

You can install the rpm and execute the service.  Puppet configurations and secrets will load the necessary variables
in /etc/systemd/system/mysqlbackup.service.d/override.conf
* You must make this PR separately on they system this will run on.




