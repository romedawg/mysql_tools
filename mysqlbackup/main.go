package main

import (
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	dateFormat     = "2006_01_02_15_04_05Z"
	snapshotFormat = "2006_01_02"
)

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableColors: true,
		FullTimestamp: true,
	})
}

func getOrCreateDayBackupDir(backupConfig *Config) (string, string, error) {

	if err := os.MkdirAll(backupConfig.BackupDir, 0700); err != nil {
		return "", "", errors.Errorf("Directory %s does not exist and cannot create it", backupConfig.BackupDir)
	}

	folderTime := time.Now().UTC().Format("2006-01-02")
	dayBackupDir := filepath.Join(backupConfig.BackupDir, folderTime)
	if err := os.MkdirAll(dayBackupDir, 0700); err != nil {
		return dayBackupDir, folderTime, err
	}
	return dayBackupDir, folderTime, nil
}

func doesFullBackupDirExist(backupDir string) bool {
	files, err := ioutil.ReadDir(backupDir)
	if err != nil {
		return false
	}

	for _, fi := range files {
		if fi.IsDir() {
			return true
		}
	}
	return false
}

func executeBackup(s3Session *session.Session, backupConfig *Config) {
	backupDir, folderTime, err := getOrCreateDayBackupDir(backupConfig)

	if err != nil {
		log.Errorf("creating a backup failed: %+v\n", err)
		return
	}

	if !doesFullBackupDirExist(backupDir) {
		backupConfig.SnapshotTime = time.Now().UTC().Format(snapshotFormat)
		fullBackupdir := filepath.Join(backupDir, time.Now().UTC().Format(dateFormat))
		if err := fullBackup(fullBackupdir, folderTime, s3Session, backupConfig); err != nil {
			log.Errorf("full backup failed for %s: %+v", fullBackupdir, err)
		}
		return
	}

	if err := incrementalBackup(backupDir, folderTime, backupConfig.IncrementalInterval, s3Session, backupConfig); err != nil {
		log.Errorf("incremental backup failed for %s: %+v", backupDir, err)
		return
	}
}

type Config struct {
	BackupDir           string
	S3Dir               string
	BackupEnv           string
	IncrementalInterval time.Duration
	Bucketname          string
	AwsRegion           string
	MysqlUser           string
	MysqlPassword       string
	SnapshotTime        string
}

func getBackupConfig() (*Config, error) {
	config := &Config{}
	var (
		backupDir           = flag.String("backup_dir", "/opt/mysql_backups", "set the MySQL backup directory to use for backups")
		incrementalInterval = flag.Duration("incremental_interval", time.Minute*60, "incremental backup intervals, use -i to set the interval(i.e 60s, 60m, 1h, etc...)")
		awsRegion           = flag.String("aws_region", "us-east-2", "set the region, default is us-east-2.")
		backupEnv           = flag.String("env", "", "set the environment(qa, uat, prod).")
		bucketName          = flag.String("bucket_name", "", "set the S3 Bucket.")
		mysqlUser           = flag.String("mysql_user", "root", "set the MySQL username")
		debug               = flag.Bool("debug", false, "change log level to debug")
	)
	flag.Parse()
	config.BackupDir = *backupDir
	config.S3Dir = filepath.Join(config.BackupDir, "s3_backups")
	config.BackupEnv = *backupEnv
	if config.BackupEnv == "" {
		return nil, errors.New("env flag is not set and it is a required flag")
	}
	config.IncrementalInterval = *incrementalInterval
	config.Bucketname = *bucketName
	if config.Bucketname == "" {
		return nil, errors.New("bucket_name flag is not set")
	}
	config.AwsRegion = *awsRegion
	config.MysqlUser = *mysqlUser
	config.MysqlPassword = os.Getenv("MYSQL_PASSWORD")
	if config.MysqlPassword == "" {
		return nil, errors.New("environment variable MYSQL_PASSWORD is not set")
	}

	if *debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}

	return config, nil
}

func main() {

	backupConfig, err := getBackupConfig()
	if err != nil {
		log.Errorf("required command line flags or environment variables are not set: %v\n", err)
		flag.Usage()
		os.Exit(1)
	}

	s3Session := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(backupConfig.AwsRegion),
	}))

	for {
		executeBackup(s3Session, backupConfig)
		time.Sleep(time.Second * 1)
	}
}
