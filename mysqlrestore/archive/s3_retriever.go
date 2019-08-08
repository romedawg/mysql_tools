package archive

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"bb.dev.norvax.net/dep/operator/backups/mysqlrestore/execute"
)

// Used in the Get method to download the snapshot.
// Can be nil if used w/ the Prepare method.
type S3Retriever struct {
	Bucket   string
	Snapshot string
}

func (s *S3Retriever) Get(ctx context.Context, restoreDir string) error {
	if err := download(ctx, s.Bucket, s.Snapshot, restoreDir); err != nil {
		return errors.Wrapf(err, "failed to download backups from snapshot %s ", s.Snapshot)
	}

	return nil
}

func (s *S3Retriever) Prepare(ctx context.Context, restoreDir string) error {
	return Prepare(ctx, restoreDir)
}

func Prepare(ctx context.Context, restoreDir string) error {
	if err := untar(ctx, restoreDir); err != nil {
		return errors.Wrapf(err, "failed to untar files in snapshot directory %s", restoreDir)
	}
	return nil
}

func untar(ctx context.Context, restoreDir string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	tarFileDir, err := ioutil.ReadDir(restoreDir)
	if err != nil {
		return err
	}

	tarFiles := []string{}
	for _, tarFileName := range tarFileDir {
		tarFiles = append(tarFiles, filepath.Join(restoreDir, tarFileName.Name()))
	}
	log.Debugf("list of backups to prepare: %s", tarFiles)

	group, _ := errgroup.WithContext(ctx)
	for _, file := range tarFiles {
		fileTemp := file
		defer os.Remove(fileTemp)
		wrap := func() error {
			log.Debugf("untarring %s", fileTemp)
			prepareCmdLine := []string{
				"tar",
				"-xzf",
				fileTemp,
				"--directory",
				restoreDir,
			}
			return execute.CmdRun(ctx, prepareCmdLine)
		}
		group.Go(wrap)
	}
	done := make(chan error)
	go func() {
		done <- group.Wait()
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-done:
		return err
	case stopSignal := <-stop:
		fmt.Printf("program received OS signal %v so it is shutting down\n", stopSignal)
		err := <-done
		return errors.Wrap(err, "untar failed")
	}
}

func download(ctx context.Context, bucket, snapshot, restoreDir string) error {
	log.Infof("downloading snapshot %v, to directory: %v", snapshot, restoreDir)
	if snapshot == "" {
		return errors.New("snapshot flag is not set so a restore cannot be performed")
	}

	if err := os.MkdirAll(restoreDir, 0700); err != nil {
		return errors.Wrap(err, "failed to create restore directory")
	}

	log.Debug("creating s3client")
	s3Client, err := execute.GetS3Client()
	if err != nil {
		return errors.Wrap(err, "failed to create s3 client")
	}

	snapshotFiles, err := listBucketFiles(s3Client, bucket, snapshot)
	if err != nil {
		return errors.Wrap(err, "failed to get list of snapshotFiles for snapshot in bucket")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	downloader := s3manager.NewDownloaderWithClient(s3Client)
	group, _ := errgroup.WithContext(ctx)
	for _, b := range snapshotFiles {
		baseName := filepath.Base(b)
		localPath := filepath.Join(restoreDir, baseName)
		log.Debugf("creating local file of backup to download to: %s", localPath)
		localFile, err := os.Create(localPath)
		if err != nil {
			return errors.Wrapf(err, "failed to create dir fo %s", localPath)
		}

		input := &s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(b),
		}

		wrap := func() error {
			log.Debugf("Downloading: %s", input)
			_, err := downloader.DownloadWithContext(ctx, localFile, input)
			return err
		}
		group.Go(wrap)
	}
	// buffered channel is necessary to avoid losing signals accidentally, consult the docks if making a change here.
	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)
	done := make(chan error)
	go func() {
		done <- group.Wait()
	}()
	select {
	case err := <-done:
		return err
	case stopSignal := <-stopCh:
		log.Errorf("program received os signal %v so it is shutting down", stopSignal)
		cancel()
		err := <-done
		return errors.Wrap(err, "download failed")
	}
}

func listBucketFiles(s3Client *s3.S3, bucket, prefix string) ([]string, error) {
	params := &s3.ListObjectsInput{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}

	log.Debugf("params for ListObject func: %s", params)
	resp, err := s3Client.ListObjects(params)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list objects in s3 bucket %s", bucket)
	}

	var objects []string
	for _, k := range resp.Contents {
		if strings.Contains(*k.Key, ".tgz") {
			objects = append(objects, *k.Key)
		}
	}
	if len(objects) == 0 {
		log.Errorf("response from s3Client.ListObjects resp object is empty, check snapshot passed in: %s", objects)
	}

	return objects, nil
}
