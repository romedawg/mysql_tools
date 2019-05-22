package restore

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/mholt/archiver"
	"github.com/pkg/errors"
	"github.com/prometheus/common/log"
	"github.com/stretchr/testify/require"

	"bb.dev.norvax.net/dep/operator/backups/mysqlrestore/archive"
)

var (
	userName      = "root"
	password      = "root123"
	dbNames       = []string{"full_backup_test", "incremental_backup_test"}
	mysqlDataDir  = "/var/lib/mysql"
	rootBackupDir = "/opt/mysql_backup"
)

func TestBackup(t *testing.T) {
	if _, err := exec.LookPath("mysqld"); err != nil {
		t.Skip()
	}
	assert := require.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()
	db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@/", userName, password))
	assert.NoError(err, "fail to open db connection")
	defer db.Close()
	t.Log("creating db full_backup_test")
	assert.NoError(createDb(ctx, db, dbNames[0]))

	backupDir := createBackupDir()
	assert.NoError(createBackup(ctx, backupDir, "full"))
	assert.NoError(archiveBackup(ctx, backupDir, "fullbackup"))

	t.Log("creating db incremental_backup_test")
	assert.NoError(createDb(ctx, db, dbNames[1]))

	incrementalDir := createBackupDir()
	assert.NoError(createBackup(ctx, incrementalDir, "incremental"))
	assert.NoError(archiveBackup(ctx, incrementalDir, "incremental"))

}

type MockPreparer struct {
	archive.S3Retriever
}

func (*MockPreparer) Get(ctx context.Context, restoreDir string) error {
	// noop
	return nil
}

func TestRestore(t *testing.T) {
	if _, err := exec.LookPath("mysqld"); err != nil {
		t.Skip()
	}

	assert := require.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()

	db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@/", userName, password))
	assert.NoError(err, "fail to open db connection")
	defer db.Close()

	assert.NoError(Snapshot(ctx, &MockPreparer{}, rootBackupDir, mysqlDataDir), "failed to restore snapshots")
}

func createBackupDir() string {
	dayBackupDir := time.Now().UTC().Format("2006_01_02_15_04_05Z")

	if _, err := os.Stat(dayBackupDir); os.IsNotExist(err) {
		err := os.MkdirAll(filepath.Join(rootBackupDir, dayBackupDir), 0700)
		if err != nil {
			return dayBackupDir
		}
	}

	return dayBackupDir
}

func clearArchiveDir(dir string) error {
	dirRead, err := os.Open(dir)
	if err != nil {
		return errors.Wrapf(err, "could not read dir %s", dir)
	}
	dirFiles, err := dirRead.Readdirnames(0)
	if err != nil {
		return errors.Wrapf(err, "could not get a directory list from dir %s", dirFiles)
	}

	log.Debugf("removing contents from %s", dir)
	for _, dirName := range dirFiles {
		os.RemoveAll(filepath.Join(dir, dirName))
	}
	os.Remove(dir)
	return nil
}

func archiveBackup(ctx context.Context, dir string, backupType string) error {

	err := os.MkdirAll(rootBackupDir, 0700)
	if err != nil {
		return errors.Wrapf(err, "could not create %s", dir)
	}

	tar := archiver.Tar{}
	fmt.Printf("source tar %s\n", filepath.Join(rootBackupDir, dir))
	fmt.Printf("destination %s\n", filepath.Join(rootBackupDir, fmt.Sprintf("%s.tar", backupType)))
	err = tar.Archive([]string{dir}, fmt.Sprintf("%s/%s.tar", rootBackupDir, backupType))
	if err != nil {
		return errors.Wrapf(err, "failed to archive dir %s", dir)
	}

	fmt.Printf("archiving.. removing %s\n", dir)
	err = clearArchiveDir(filepath.Join(rootBackupDir, dir))
	if err != nil {
		return errors.Wrapf(err, "failed to remove %s", dir)
	}
	return nil

}

func createDb(ctx context.Context, db *sql.DB, dbName string) error {
	query := fmt.Sprintf("CREATE DATABASE %s", dbName)
	stmt, err := db.PrepareContext(ctx, query)
	if err != nil {
		return errors.Wrapf(err, "failed to prepare SQL statement %q", query)
	}

	defer stmt.Close()
	_, err = stmt.Exec()
	return errors.Wrapf(err, "failed to create database with SQL statement")

}

func runCmd(ctx context.Context, cmdLine []string) error {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, cmdLine[0], cmdLine[1:]...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return errors.Wrapf(err, "command failed %v, stdout: %s, stderr: %s", cmdLine, stdout.String(), stderr.String())
	}
	return nil
}

func createBackup(ctx context.Context, backupDir string, backupType string) error {

	err := os.MkdirAll(backupDir, 0777)
	if err != nil {
		return errors.Wrapf(err, "could not create %s", backupDir)
	}

	//Does a full bakcup exists?
	fullBackupDir, err := ioutil.ReadDir(rootBackupDir)
	if err != nil {
		return errors.Wrapf(err, "could not read %s", rootBackupDir)
	}

	if len(fullBackupDir) == 0 {
		cmdLines := []string{
			"innobackupex",
			fmt.Sprintf("--user=%s", userName),
			fmt.Sprintf("--password=%s", password),
			"--compress",
			"--slave-info",
			"--safe-slave-backup",
			"--no-timestamp",
			backupDir,
		}
		err = runCmd(ctx, cmdLines)
		if err != nil {
			return errors.Wrapf(err, "failed to perpare backup")
		}
		return nil
	}

	cmdLines := []string{
		"innobackupex",
		fmt.Sprintf("--user=%s", userName),
		fmt.Sprintf("--password=%s", password),
		"--compress",
		"--slave-info",
		"--safe-slave-backup",
		"--no-timestamp",
		fullBackupDir[0].Name(), fmt.Sprintf("--incremental=%s", backupDir),
	}
	err = runCmd(ctx, cmdLines)
	if err != nil {
		return errors.Wrapf(err, "failed to perpare backup")
	}
	return nil

}
