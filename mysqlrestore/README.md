# mysqlrestore

## What is this used for?
Used to list snapshots(full and incremental backups) that are available on s3 

You can also restore mysql to a point in time buy providing a snapshot that is available on the previously
noted s3 bucket.

## Why is this necessary?
Restoring MySql can be tedious and error prone.  We are also actively sending mysql backups to s3 so this is a good way to 
restore and verify backups.




