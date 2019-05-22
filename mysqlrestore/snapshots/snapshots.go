package snapshots

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// Returns a json list of the snapshots available in s3 based on environment passed in at runtime.
func ListSnapshot(env, bucket string) ([]byte, error) {
	s3Client, err := getS3Client()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create s3 client")
	}

	params := &s3.ListObjectsInput{
		Bucket: aws.String(bucket),
		Prefix: aws.String(filepath.Join(env)),
	}
	log.Debugf("s3 request bucket: %s, prefix: %s", *params.Bucket, *params.Prefix)
	resp, err := s3Client.ListObjects(params)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to list objects in s3 bucket %s", bucket)
	}
	snapshotKeys := make(map[string]struct{})
	for _, key := range resp.Contents {
		snapshot := *key.Key
		if strings.Contains(snapshot, "snapshot") && strings.Contains(snapshot, ".tar") {
			key := filepath.Dir(snapshot)
			if _, ok := snapshotKeys[key]; !ok {
				snapshotKeys[key] = struct{}{}
			}
		}
	}
	log.Debugf("found snapshotKeys: %v", snapshotKeys)
	var snapshots []string
	for k := range snapshotKeys {
		snapshots = append(snapshots, k)
	}

	jsonSnapshots, err := json.MarshalIndent(snapshots, " ", "")
	if err != nil {
		return nil, errors.Wrapf(err, "could not generate json of s3 snapshots")
	}
	return jsonSnapshots, nil
}

func getS3Client() (*s3.S3, error) {
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if secretKey == "" {
		return nil, errors.New("environment variable AWS_SECRET_ACCESS_KEY is not set")
	}
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	if accessKey == "" {
		return nil, errors.New("environment variable AWS_ACCESS_KEY_ID is not set")
	}
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-2"
	}
	token := os.Getenv("AWS_SESSION_TOKEN")
	creds := credentials.NewStaticCredentials(accessKey, secretKey, token)
	cfg := aws.NewConfig().WithRegion(region).WithCredentials(creds)
	sess, err := session.NewSession(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create aws session")
	}
	return s3.New(sess), nil
}
