package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func fullBackup(backupDir string, folderTime string, s3Session *session.Session, backupConfig *Config) error {
	log.Infof("Creating full back up in directory: %s", backupDir)
	log.Infof("Snapshot name: snapshot_%s", backupConfig.SnapshotTime)

	err := os.MkdirAll(backupDir, 0700)
	if err != nil {
		return err
	}

	cmdLine := []string{
		"innobackupex",
		fmt.Sprintf("--user=%+s", backupConfig.MysqlUser),
		fmt.Sprintf("--password=%+s", backupConfig.MysqlPassword),
		"--slave-info",
		"--safe-slave-backup",
		"--compress",
		backupDir,
		"--no-timestamp",
		"--compress-threads=8",
		"--parallel=8",
		"--use-memory=2G"}
	cmd := exec.Command(cmdLine[0], cmdLine[1:]...)
	log.Infof("Executing command: %s", strings.Join(cmdLine, " "))

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return errors.Wrapf(err, "cmd failed %s, stderr: %s", strings.Join(cmdLine, " "), stderr.String())
	}

	err = archiveBackupToS3(backupDir, "full", folderTime, s3Session, backupConfig)
	if err != nil {
		return errors.Wrapf(err, "failed to archive backup %s to s3 bucket", backupDir)
	}

	log.Infof("successfully created full back up in directory: %s", backupDir)
	return nil
}

func incrementalBackupTimeCheck(backupDir string, dur time.Duration) (bool, error) {
	baseName := filepath.Base(backupDir)
	baseTime, err := time.Parse(dateFormat, baseName)
	if err != nil {
		return false, err
	}
	currentTime := time.Now().UTC()
	dif := currentTime.Sub(baseTime.UTC())

	return dif > dur, nil
}

func incrementalBackup(backupDir string, folderTime string, dur time.Duration, s3Session *session.Session, backupConfig *Config) error {

	increBackupDir := filepath.Join(backupDir, time.Now().UTC().Format(dateFormat))

	files, err := ioutil.ReadDir(backupDir)
	if err != nil {
		return errors.Wrapf(err, "could not read directory: %v\n", backupDir)
	}

	previousBackupDir := files[len(files)-1]
	previousBackup := path.Join(backupDir, previousBackupDir.Name())
	if !previousBackupDir.IsDir() {
		return errors.Errorf("attempted to find a previous backup directory. Found %s but that is not a directory", previousBackupDir)
	}

	res, err := incrementalBackupTimeCheck(previousBackup, dur)
	if err != nil {
		return errors.Wrap(err, "unable to determine whether an incremental backup should be made")
	}

	if !res {
		return nil
	}

	log.Infof("Creating an incremental backup in %s because the last back up was made over %v ago", increBackupDir, dur)
	log.Infof("Snapshot name: snapshot_%s", backupConfig.SnapshotTime)

	var stderr bytes.Buffer
	cmdLine := []string{
		"innobackupex",
		fmt.Sprintf("--user=%+s", backupConfig.MysqlUser),
		fmt.Sprintf("--password=%+s", backupConfig.MysqlPassword),
		"--slave-info",
		"--safe-slave-backup",
		"--incremental",
		"--compress",
		increBackupDir,
		fmt.Sprintf("--incremental-basedir=%s", previousBackup),
		"--no-timestamp",
		"--compress-threads=8",
		"--parallel=8",
		"--use-memory=2G"}
	cmd := exec.Command(cmdLine[0], cmdLine[1:]...)
	log.Infof("executing command: %s", strings.Join(cmdLine, " "))
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return errors.Wrapf(err, "cmd failed %s\nstderr: %s\n", strings.Join(cmdLine, " "), stderr.String())
	}

	err = archiveBackupToS3(increBackupDir, "incremental", folderTime, s3Session, backupConfig)
	if err != nil {
		return errors.Wrapf(err, "failed to archive backup %s to s3 bucket", backupDir)
	}
	log.Infof("successfully created incremental backup in %s", increBackupDir)
	return err
}

func archiveBackupToS3(backupDir string, backupType string, folderTime string, s3Session *session.Session, backupConfig *Config) error {

	// Create tar file
	tarFile, err := tarBackup(backupDir, backupType+"_", backupConfig)
	if err != nil {
		return errors.Wrapf(err, "could not create a tarfile of %v\n", backupDir)
	}

	// Delete tar file after it is uploaded to S3 bucket.
	defer func() {
		if fi, err := os.Stat(filepath.Join(backupConfig.S3Dir, tarFile)); err == nil && !fi.IsDir() {
			os.Remove(filepath.Join(backupConfig.S3Dir, tarFile))
			log.Debugf("removing tarfile: %s, which has been uploaded to S3", filepath.Join(backupConfig.S3Dir, tarFile))
		}
	}()

	err = uploadS3Bucket(s3Session, tarFile, folderTime, backupConfig)
	if err != nil {
		return errors.Wrapf(err, "failed to uploaded tar file %s to s3 bucket %s", tarFile, backupConfig.Bucketname)
	}
	return nil

}

func tarBackup(targetDir, backupType string, backupConfig *Config) (string, error) {

	if err := os.MkdirAll(backupConfig.S3Dir, 0700); err != nil {
		return "", errors.Wrapf(err, "Directory %s does not exist and cannot create it", backupConfig.S3Dir)
	}

	tarFileName := backupType + filepath.Base(targetDir) + ".tar"

	log.Infof("Tarring directory %s into file %s", targetDir, tarFileName)
	tarCmdLine := []string{"tar", "-cf", backupConfig.S3Dir + "/" + tarFileName, "--directory=" + filepath.Dir(targetDir), filepath.Base(targetDir)}
	tarCmd := exec.Command(tarCmdLine[0], tarCmdLine[1:]...)
	var stderr, stdout bytes.Buffer
	tarCmd.Stderr = &stderr
	tarCmd.Stdout = &stdout

	log.Infof("Executing command: %s", strings.Join(tarCmdLine, " "))
	err := tarCmd.Run()
	if err != nil {
		return "", errors.Wrapf(err, "cmd failed %s\nstderr: %s\nstdout: %s\n", strings.Join(tarCmdLine, " "), stderr.String(), stdout.String())
	}
	log.Infof("Successfully created tar file %s of backup %s", tarFileName, targetDir)

	return tarFileName, nil
}

func uploadS3Bucket(session *session.Session, tarFile string, folderTime string, backupConfig *Config) error {
	log.Infof("uploading tar file %s to s3 bucket %s", tarFile, backupConfig.Bucketname)

	uploader := s3manager.NewUploader(session)

	file, err := os.Open(filepath.Join(backupConfig.S3Dir, tarFile))
	if err != nil {
		return errors.WithStack(err)
	}

	// Dates for s3 directory structure in Key
	TIMESTAMP := time.Now().UTC()
	year := fmt.Sprintf("%v", TIMESTAMP.Year())
	month := fmt.Sprintf("%v", TIMESTAMP.Month())
	day := fmt.Sprintf("%v", TIMESTAMP.Day())
	snapshotName := "snapshot" + "_" + backupConfig.SnapshotTime

	keyName := filepath.Join("mysqlbackups", backupConfig.BackupEnv, year, month, day, snapshotName, tarFile)

	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket:               aws.String(backupConfig.Bucketname),
		Key:                  aws.String(keyName),
		Body:                 file,
		ServerSideEncryption: aws.String("AES256"),
		Tagging:              aws.String(snapshotName),
	})

	if err != nil {
		return errors.Errorf("failed to upload to s3: , %+v\n", err)
	}
	log.Infof("successfully uploaded tar file %s to s3 bucket %s", tarFile, backupConfig.Bucketname)
	log.Debugf("uploaded file %s, to bucket %s, in directory %s", tarFile, backupConfig.Bucketname, keyName)

	return nil
}
