package application

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"io"
	"math"
)

type Options struct {
	Endpoint        string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	Region          string `json:"region,omitempty" yaml:"region,omitempty"`
	DisableSSL      bool   `json:"disableSSL" yaml:"disableSSL"`
	ForcePathStyle  bool   `json:"forcePathStyle" yaml:"forcePathStyle"`
	AccessKeyID     string `json:"accessKeyID,omitempty" yaml:"accessKeyID,omitempty"`
	SecretAccessKey string `json:"secretAccessKey,omitempty" yaml:"secretAccessKey,omitempty"`
	SessionToken    string `json:"sessionToken,omitempty" yaml:"sessionToken,omitempty"`
	Bucket          string `json:"bucket,omitempty" yaml:"bucket,omitempty"`
}

const (
	DefaultPartSize = 5 << (10 * 2)
	MinConcurrency  = 5
	MaxConcurrency  = 128
)

func calculateConcurrency(size int) int {
	if size <= 0 {
		return MinConcurrency
	}
	c := int(math.Ceil(float64(size) / float64(DefaultPartSize)))
	if c < MinConcurrency {
		return MinConcurrency
	} else if c > MaxConcurrency {
		return MaxConcurrency
	}
	return c
}

func Upload(fileName, bucket string, body io.Reader, size int, c client.ConfigProvider) error {
	uploader := s3manager.NewUploader(c, func(uploader *s3manager.Uploader) {
		uploader.PartSize = DefaultPartSize
		uploader.LeavePartsOnError = true
		uploader.Concurrency = calculateConcurrency(size)
	})
	_, err := uploader.Upload(&s3manager.UploadInput{
		Bucket:             aws.String(bucket),
		Key:                aws.String(fileName),
		Body:               body,
		ContentDisposition: aws.String(fmt.Sprintf("attachment; filename=\"%s\"", fileName)),
	})
	return err
}

func download(s *session.Session, key, bucket string) ([]byte, error) {
	downloader := s3manager.NewDownloader(s)
	writer := aws.NewWriteAtBuffer([]byte{})
	_, err := downloader.Download(writer,
		&s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
	return writer.Bytes(), err
}

func getAwsSession(opt Options) (client.ConfigProvider, error) {
	config := aws.Config{
		Endpoint:         aws.String(opt.Endpoint),
		DisableSSL:       aws.Bool(true),
		S3ForcePathStyle: aws.Bool(true),
		Region:           aws.String(opt.Region),
		Credentials:      credentials.NewStaticCredentials(opt.AccessKeyID, opt.SecretAccessKey, ""),
	}
	s, err := session.NewSession(&config)
	return s, err
}
