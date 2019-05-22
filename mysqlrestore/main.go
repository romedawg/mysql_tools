package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/onrik/logrus/filename"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"bb.dev.norvax.net/dep/operator/backups/mysqlrestore/archive"
	"bb.dev.norvax.net/dep/operator/backups/mysqlrestore/restore"
	"bb.dev.norvax.net/dep/operator/backups/mysqlrestore/snapshots"
)

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableColors: true,
		FullTimestamp: true,
	})

	log.SetLevel(log.DebugLevel)
	filenameHook := filename.NewHook()
	filenameHook.Field = "source"
	log.AddHook(filenameHook)
}

var (
	op         = flag.String("operation", "", "operation to be run: list or restore")
	env        = flag.String("env", "", "environment to use(qa, ga, or prod)")
	bucket     = flag.String("bucket", "", "s3 bucket that holds mysql backups")
	snapshot   = flag.String("snapshot", "", "snapshot to be restored(if performing a restore operation")
	restoreDir = flag.String("directory", "", "restore directory to use for full and incremental backups.")
	debug      = flag.Bool("debug", false, "change log level to debug(default: false)")
	datadir    = flag.String("datadir", "/var/lib/mysql/data", "default location for mysql datadir")
)

func setup() error {
	flag.Parse()
	switch *env {
	case "qa", "ga", "ops", "prod", "testing":
	default:
		return errors.Errorf("invalid env %s.  Try qa, ga, prod", *env)
	}
	if *bucket == "" {
		return errors.New("failed to specify s3 bucket")
	}
	if *op == "restore" && *restoreDir == "" {
		return errors.New("need to specify a directory to use for full and incremental backups")
	}

	if *debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}

	return nil
}

func main() {

	ctx := context.Background()
	err := setup()
	if err != nil {
		flag.Usage()
		log.Fatalln(err)
	}
	switch *op {
	case "list":
		log.Infof("listing snapshots for %v", *env)
		jsonSnapshots, err := snapshots.ListSnapshot(*env, *bucket)
		if err != nil {
			log.Fatalln(err)
		}
		fmt.Printf("%s\n", jsonSnapshots)
		usage := strings.Builder{}
		usage.WriteString("Here are the list of snapshots, select one to use to restore to, for example:\n")
		usage.WriteString("mysqlrestore -operation restore -env prod -bucket global-backup-storage-bucket-flhspka03aso -snapshot prod/mysql/db-backup1.ops.norvax.net/2019/4/27/snapshot_2019_04_27_21_53_36Z -directory /opt/mysql_backups -debug true\n")
		fmt.Println(usage.String())
		os.Exit(0)
	case "restore":

		log.Infof("Removing contents from directory %v", restoreDir)
		if err := restore.ClearRestoreDir(*restoreDir); err != nil {
			log.Fatal(err)
		}
		log.Infof("restoring snapshot %v, in %v", *snapshot, *env)
		if err := restore.Snapshot(ctx, &archive.S3Retriever{Bucket: *bucket, Snapshot: *snapshot}, *restoreDir, *datadir); err != nil {
			log.Fatal(err)
		}
		log.Infof("Restore Complete")
		log.Infof("Starting MySQL")
		err := restore.StartMysql(ctx)
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	default:
		log.Fatalf("operation not support %s: supported operations list restore\n", *op)
	}
}
