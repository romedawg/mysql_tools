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
	"bb.dev.norvax.net/dep/operator/cli"
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
	op          = flag.String("operation", "", "operation to be run: list, restore snapshotX, latest to restore latest snapshot")
	cluster     = flag.String("cluster", "", "cluster to list or restore from(cluster one or two")
	env         = flag.String("env", "", "environment to use(dev, qa, ga, or prod)")
	bucket      = flag.String("bucket", "", "s3 bucket that holds mysql backups")
	snapshot    = flag.String("snapshot", "", "snapshot to be restored(if performing a restore operation")
	restoreDir  = flag.String("directory", "", "restore directory to use for full and incremental backups.")
	debug       = flag.Bool("debug", false, "change log level to debug(default: false)")
	datadir     = flag.String("datadir", "/var/lib/mysql/data", "default location for mysql datadir")
	versionFlag = flag.Bool("version", false, "print version information about the go binary")
	GitCommit   string
	BuildTime   string
	Semver      string
)

func setup() error {
	flag.Parse()
	if *versionFlag {
		version := &cli.Version{GitCommit: GitCommit, BuildTime: BuildTime, Semver: Semver}
		fmt.Println(version)
		os.Exit(0)
	}
	switch *env {
	case "dev", "qa", "ga", "prod", "testing":
	default:
		return errors.Errorf("invalid env %s.  Try qa, ga, prod", *env)
	}
	if *bucket == "" {
		return errors.New("failed to specify s3 bucket")
	}
	if *op == "list" && *cluster == "" {
		return errors.New("failed to specify which cluster to use(one or two).")
	}
	if *op == "restore" && *restoreDir == "" {
		return errors.New("need to specify a directory to use for full and incremental backups")
	}
	if *op == "latest" && *restoreDir == "" {
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
		log.Debugf("listing snapshots for %v\n", *env)
		snapshots, mostRecentSnapshot, err := snapshots.ListSnapshots(*env, *bucket, *cluster)
		if err != nil {
			log.Fatalln(err)
		}
		usage := strings.Builder{}
		usage.WriteString("Here are the list of snapshots.\n")
		for _, snapshot := range snapshots {
			usage.WriteString(fmt.Sprintf("Path: %+v, Snapshot: %+v, Timestamp: %+v\n", snapshot.Path, snapshot.SnapshotName, snapshot.Timestamp))
		}
		usage.WriteString("\n")
		usage.WriteString("Select one to restore from, to use the most resent, execute the following command:\n")
		txt := fmt.Sprintf("mysqlrestore -operation restore -env %s -bucket %s -snapshot %s -directory /opt/mysqlrestore -debug true", *env, *bucket, mostRecentSnapshot)
		usage.WriteString(txt)
		fmt.Println(usage.String())
		os.Exit(0)
	case "restore":
		if err := restore.ClearRestoreDir(*restoreDir); err != nil {
			log.Fatal(err)
		}
		log.Infof("restoring snapshot %v, for env: %v", *snapshot, *env)
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
	case "latest":
		log.Debugf("Generate most resent snapshot %v\n", *env)
		_, mostRecentSnapshot, err := snapshots.ListSnapshots(*env, *bucket, *cluster)
		if err != nil {
			log.Fatalln(err)
		}

		log.Debugf("Clear out %s", *restoreDir)
		if err := restore.ClearRestoreDir(*restoreDir); err != nil {
			log.Fatal(err)
		}

		log.Infof("restoring snapshot %v, for env: %v", mostRecentSnapshot, *env)
		if err := restore.Snapshot(ctx, &archive.S3Retriever{Bucket: *bucket, Snapshot: mostRecentSnapshot}, *restoreDir, *datadir); err != nil {
			log.Fatal(err)
		}
		log.Infof("Restore Complete")
		log.Infof("Starting MySQL")
		err = restore.StartMysql(ctx)
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	default:
		log.Fatalf("operation not support %s, supported operations: list or restore\n", *op)
	}
}
