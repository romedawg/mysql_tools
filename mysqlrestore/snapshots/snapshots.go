package snapshots

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bb.dev.norvax.net/dep/operator/backups/mysqlrestore/execute"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type SnapshotMeta struct {
	SnapshotName string
	Timestamp    time.Time
	Path         string
}

type snapshotSlices []SnapshotMeta

func getSnapshots(env, bucket, cluster string) ([]s3.Object, error) {
	snapshotObjects := []s3.Object{}
	s3Client, err := execute.GetS3Client()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create s3 client")
	}
	cluster_type := fmt.Sprintf("cluster_%s", cluster)
	params := &s3.ListObjectsInput{
		Bucket: aws.String(bucket),
		//Prefix: aws.String(filepath.Join(env)),
		Prefix: aws.String(filepath.Join(env, "mysql", cluster_type)),
		// We only care about snapshot groups w/ full backups. This also helps limit the returns.
		Delimiter: aws.String("incremental_backup"),
	}
	log.Debugf("s3 request bucket: %s, prefix: %s", *params.Bucket, *params.Prefix)
	resp, err := s3Client.ListObjects(params)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list objects in s3 bucket %s", bucket)
	}

	for _, key := range resp.Contents {
		log.Debugf("s3 object key names are %s", *key.Key)
		if strings.HasSuffix(*key.Key, "full_backup") {
			log.Infof("key name with full backup hasSuffix %s", *key.Key)
		}
		snapshotObjects = append(snapshotObjects, *key)
	}

	return snapshotObjects, nil
}

// Returns a json list of the snapshots available in s3 based on environment passed in at runtime.
func ListSnapshots(env, bucket, cluster string) ([]SnapshotMeta, string, error) {

	var snapshotList []SnapshotMeta
	//var snapshotList []string
	snapshots, err := getSnapshots(env, bucket, cluster)
	if err != nil {
		return nil, "", errors.WithStack(err)
	}

	for _, snapshot := range snapshots {
		snapshotkey := strings.Split(*snapshot.Key, "/")
		s3UrlPrefix := strings.Join(snapshotkey[0:5], "/")
		snapshotList = append(snapshotList, SnapshotMeta{snapshotkey[5], *snapshot.LastModified, s3UrlPrefix})
	}

	log.Debugf("snapshot object %s", snapshotList)
	sortedSnapshots := sortSnapshots(snapshotList)

	// Building this string for convenience when outputting latest snapshot
	if len(sortedSnapshots) == 0 {
		log.Infof("there are no snapshots available, check bucket: %s", bucket)
		os.Exit(1)
	}
	snapshotLen := len(sortedSnapshots)
	mostRecentSnapshot := fmt.Sprintf("%s/mysql/cluster_%s/%d/%d/%s", env, cluster, sortedSnapshots[snapshotLen-1].Timestamp.Year(), sortedSnapshots[snapshotLen-1].Timestamp.Month(), sortedSnapshots[snapshotLen-1].SnapshotName)

	return sortedSnapshots, mostRecentSnapshot, nil
}

func (s snapshotSlices) Len() int {
	return len(s)
}

func (s snapshotSlices) Less(i, j int) bool {
	return s[i].Timestamp.Before(s[j].Timestamp)
}

func (s snapshotSlices) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func sortSnapshots(snapshots []SnapshotMeta) []SnapshotMeta {

	sortedSnapshots := make(snapshotSlices, 0, len(snapshots))
	for _, snapshot := range snapshots {
		sortedSnapshots = append(sortedSnapshots, snapshot)
	}

	sort.Sort(sortedSnapshots)
	return sortedSnapshots
}
