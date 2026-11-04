package release

import (
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
}

type S3Uploader struct {
	bucket   string
	uploader *manager.Uploader
}

func NewS3Uploader(cfg S3Config) *S3Uploader {
	client := s3.New(s3.Options{
		Region:       cfg.Region,
		BaseEndpoint: aws.String(cfg.Endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		UsePathStyle: true,
		// OVH and Scaleway reject the CRC32 trailers the SDK sends by default since v1.73.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	return &S3Uploader{bucket: cfg.Bucket, uploader: manager.NewUploader(client)}
}

func (u *S3Uploader) Put(ctx context.Context, key string, body io.ReadSeeker, _ int64) error {
	_, err := u.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(u.bucket),
		Key:    aws.String(key),
		Body:   body,
		ACL:    types.ObjectCannedACLPublicRead,
	})
	return err
}
