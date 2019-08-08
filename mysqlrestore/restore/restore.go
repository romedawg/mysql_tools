package restore

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"bb.dev.norvax.net/dep/operator/backups/mysqlrestore/execute"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// Snapshot interface that splits up the restore into two Method.
// Get: downloads the snapshot passed in at runtime.
// Prepare: Will untar the snapshot(full and incrementals)
type SnapshotRetriever interface {
	Get(ctx context.Context, restoreDir string) error

	Prepare(ctx context.Context, restoreDir string) error
}

// Generic function that is called in main.go to perform a full snapshot restore.
// Combines all the functions in the mysqlrestore module(download, untar, decompress, prepare, etc..).
func Snapshot(ctx context.Context, retriever SnapshotRetriever, restoreDir string, datadir string) error {

	log.Debug("Downloading snapshot..")
	if err := retriever.Get(ctx, restoreDir); err != nil {
		return errors.Wrap(err, "failed to get snapshot from archive")
	}

	log.Infof("Untar backups")
	if err := retriever.Prepare(ctx, restoreDir); err != nil {
		return errors.Wrap(err, "failed to get snapshot from archive")
	}

	log.Debugf("Decompressing snapshots in %s", restoreDir)
	if err := decompressMySQLFiles(ctx, restoreDir); err != nil {
		return errors.Wrapf(err, "failed to decompressMySQLFiles snapshots")
	}

	log.Debugf("Preparing snapshots")
	fullBackupDir, err := prepare(ctx, restoreDir)
	if err != nil {
		return errors.Wrapf(err, "failed to prepare snapshots")
	}

	log.Infof("Moving full backupdir %s to %s", fullBackupDir, datadir)
	if err = moveFullBackup(fullBackupDir, datadir); err != nil {
		return errors.Wrapf(err, "unable to move full backup from %s to /var/lib/mysql/data", restoreDir)
	}

	log.Infof("Chown mysql:mysql %s", datadir)
	if err := chownMysqlDir(datadir); err != nil {
		return errors.Wrapf(err, "unable to move full backup from %s to %s", restoreDir, datadir)
	}

	return nil
}

// Will ensure that the restore directory that is passed in at runtime is empty.
// This is helpful when untar's and mysql prepares run.
func ClearRestoreDir(restoreDir string) error {

	if _, err := os.Stat(restoreDir); os.IsNotExist(err) {
		err = os.Mkdir(restoreDir, 0700)
		if err != nil {
			return errors.Wrapf(err, "could not create dir %s", restoreDir)
		}
	}

	dirRead, err := os.Open(restoreDir)
	if err != nil {
		return errors.Wrapf(err, "could not read dir %s", restoreDir)
	}
	dirFiles, err := dirRead.Readdirnames(0)
	if err != nil {
		return errors.Wrapf(err, "could not get a directory list from dir %s", dirFiles)
	}

	log.Debugf("Delete all files from %s", restoreDir)
	for _, dirName := range dirFiles {
		os.RemoveAll(filepath.Join(restoreDir, dirName))
	}

	return nil
}

func decompressMySQLFiles(ctx context.Context, restoreDir string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if _, err := os.Stat("/usr/local/bin/decompress_mysql_snapshot.sh"); os.IsNotExist(err) {
		return errors.Wrapf(err, "/usr/local/bin/decompress_mysql_snapshot.sh does not exist on the server")
	}

	cmdLine := []string{"/usr/local/bin/decompress_mysql_snapshot.sh", restoreDir}

	log.Debugf("executing %s on directories %s", cmdLine, restoreDir)
	err := execute.CmdRun(ctx, cmdLine)
	if err != nil {
		return errors.Wrapf(err, "cmd failed")
	}
	log.Infof("Successfully decompressed directories in %s", restoreDir)

	return nil
}

func prepare(ctx context.Context, restoreDir string) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	backupDirectories, err := ioutil.ReadDir(restoreDir)
	if err != nil {
		return "", err
	}

	if len(backupDirectories) == 0 {
		return "", errors.New("backup Directory is empty")
	}
	var snapshotDir []string
	for _, backupFile := range backupDirectories {
		snapshotDir = append(snapshotDir, filepath.Join(restoreDir, backupFile.Name()))
	}
	log.Debugf("list of backups that need to be prepared for mysql restore: %s", snapshotDir)

	fullBackupDir := snapshotDir[0]
	if len(snapshotDir[0]) == 0 {
		log.Errorf("Snapshot directory empty, could not find a full backup in %s", fullBackupDir)
	}
	log.Debugf("preparing full backup %s", fullBackupDir)
	prepareCmdLine := []string{
		"innobackupex",
		"--apply-log",
		"--redo-only",
		fmt.Sprintf("---parallel=%d", runtime.NumCPU()),
		"--use-memory=2G", fullBackupDir,
	}

	err = execute.CmdRun(ctx, prepareCmdLine)
	if err != nil {
		return "", errors.Wrapf(err, "cmd failed %s", strings.Join(prepareCmdLine, " "))
	}

	for _, incrementalBackupPath := range snapshotDir[1:] {
		log.Debugf("preparing incrementalBackupPath for incremental backups %s", incrementalBackupPath)
		prepareCmdLine := []string{
			"innobackupex",
			"--apply-log",
			"--redo-only",
			fmt.Sprintf("---parallel=%d", runtime.NumCPU()),
			"--use-memory=2G",
			fullBackupDir, fmt.Sprintf("--incremental=%s", incrementalBackupPath),
		}

		err := execute.CmdRun(ctx, prepareCmdLine)
		if err != nil {
			return "", errors.Wrapf(err, "cmd failed %s", strings.Join(prepareCmdLine, " "))
		}
	}

	log.Infof("Successfully prepared directory %s", restoreDir)
	return fullBackupDir, nil
}

func moveFullBackup(fullBackupDir string, mysqlDataDir string) error {

	if _, err := os.Stat(mysqlDataDir); os.IsNotExist(err) {
		err := os.MkdirAll(mysqlDataDir, 0700)
		if err != nil {
			return errors.Wrapf(err, "could not create directory %s", mysqlDataDir)
		}
	}
	walkFn := func(archivePath string, info os.FileInfo, walkErr error) error {
		if info.IsDir() {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		dstPath := strings.TrimPrefix(archivePath, fullBackupDir)

		if err := os.MkdirAll(path.Dir(path.Join(mysqlDataDir, dstPath)), 0700); err != nil {
			return errors.Wrapf(err, "could not create directory %s", mysqlDataDir)
		}

		dstPath = filepath.Join(mysqlDataDir, dstPath)
		dstFile, err := os.Create(dstPath)
		if err != nil {
			return errors.Wrap(err, "failed to create destination path")
		}
		archiveFile, err := os.Open(archivePath)
		if err != nil {
			return errors.Wrap(err, "failed to create archive file")
		}
		_, err = io.Copy(dstFile, archiveFile)

		return errors.Wrapf(err, "failed to copy file from %v to %v", archiveFile, dstFile)
	}
	return filepath.Walk(fullBackupDir, walkFn)
}

func chownMysqlExecute(path string, uid int, gid int) error {
	return filepath.Walk(path, func(name string, info os.FileInfo, err error) error {
		if err == nil {
			err = os.Chown(name, uid, gid)
		}
		return err
	})
}

func chownMysqlDir(mysqlDataDir string) error {

	mysqlUser, err := user.Lookup("mysql")
	if err != nil {
		return errors.Wrapf(err, "failed to look up user mysql")
	}

	mysqlUID, err := strconv.Atoi(mysqlUser.Uid)
	if err != nil {
		return err
	}

	mysqlGID, err := strconv.Atoi(mysqlUser.Gid)
	if err != nil {
		return err
	}

	log.Debugf("chown -R mysql:mysql for %s, uid: %d, gui: %d", mysqlDataDir, mysqlUID, mysqlGID)
	err = chownMysqlExecute(mysqlDataDir, mysqlUID, mysqlGID)
	if err != nil {
		return errors.Wrapf(err, "could not chwon %s", mysqlDataDir)
	}
	return nil
}

// Will execute 'systemctl start mysqld' once the snapshot is restored.
func StartMysql(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	log.Infof("starting mysql")
	startMysqlCmdLine := []string{"/bin/systemctl", "start", "mysql"}
	err := execute.CmdRun(ctx, startMysqlCmdLine)

	if err != nil {
		return errors.Wrapf(err, "cmd failed %s.", startMysqlCmdLine)
	}
	return nil
}
