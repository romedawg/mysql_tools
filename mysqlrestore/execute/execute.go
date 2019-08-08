package execute

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type awsSecurityCreds struct {
	Code            string
	LastUpdated     string
	Type            string
	AccessKeyId     string
	SecretAccessKey string
	Token           string
	Expiration      string
}

func awsMetaDataRequest(url string) ([]byte, error) {
	httpRes, err := http.DefaultClient.Get(url)
	if err != nil {
		return nil, errors.Wrapf(err, "GET request failed for url: %s", url)
	}
	httpResponse, err := ioutil.ReadAll(httpRes.Body)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read http reponse: %s", httpRes.Body)
	}

	return httpResponse, nil
}

func GetS3Client() (*s3.S3, error) {

	iamRoleURL := "http://169.254.169.254/latest/meta-data/iam/security-credentials"
	iamRole, err := awsMetaDataRequest(iamRoleURL)
	if err != nil {
		errors.Cause(err)
	}
	log.Debugf("AWS iam role is %s\n", string(iamRole))

	iamCredentialsUrl := "http://169.254.169.254/latest/meta-data/iam/security-credentials/" + string(iamRole)
	securityCredResp, err := awsMetaDataRequest(iamCredentialsUrl)
	if err != nil {
		errors.Cause(err)
	}
	awsCreds := &awsSecurityCreds{}
	err = json.Unmarshal(securityCredResp, awsCreds)
	if err != nil {
		panic(err)
	}

	creds := credentials.NewStaticCredentials(awsCreds.AccessKeyId, awsCreds.SecretAccessKey, awsCreds.Token)
	cfg := aws.NewConfig().WithRegion("us-east-2").WithCredentials(creds)
	sess, err := session.NewSession(cfg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create aws session")
	}
	return s3.New(sess), nil
}

func CmdRun(ctx context.Context, cmdLine []string) error {
	var stderr, stdout bytes.Buffer
	procCtx, procCancel := context.WithCancel(ctx)
	defer procCancel()
	cmd := exec.CommandContext(procCtx, cmdLine[0], cmdLine[1:]...)

	cmd.Stderr = &stderr
	cmd.Stdout = &stdout

	err := cmd.Start()
	if err != nil {
		return errors.Wrapf(err, "could not execute cmd.Start")
	}
	done := make(chan error)
	go func() {
		done <- cmd.Wait()
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case <-procCtx.Done():
		procCancel()
		return procCtx.Err()
	case <-stop:
		procCancel()
		err := <-done
		return errors.Wrapf(err, "command failed %v %s %s", cmdLine, stderr.String(), stdout.String())
	case err := <-done:
		procCancel()
		return errors.Wrapf(err, "command failed %v %s %s", cmdLine, stderr.String(), stdout.String())
	}
}
