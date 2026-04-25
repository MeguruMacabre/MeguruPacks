package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"strings"

	"github.com/MeguruMacabre/MeguruPacks/internal/appconfig"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

type ObjectInfo struct {
	Key  string
	Size int64
}

type Client struct {
	bucket string
	prefix string
	api    *s3.Client
}

func New(ctx context.Context, cfg appconfig.Config) (*Client, error) {
	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.S3.Region),
	}

	if cfg.HasS3Credentials() {
		loadOpts = append(loadOpts,
			awsconfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(
					cfg.S3.AccessKeyID,
					cfg.S3.SecretKey,
					cfg.S3.SessionToken,
				),
			),
		)
	} else {
		loadOpts = append(loadOpts,
			awsconfig.WithCredentialsProvider(aws.AnonymousCredentials{}),
		)
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.S3.Endpoint)
		o.UsePathStyle = cfg.S3.PathStyle
		o.DisableLogOutputChecksumValidationSkipped = true
	})

	return &Client{
		bucket: cfg.S3.Bucket,
		prefix: cfg.S3.Prefix,
		api:    client,
	}, nil
}

func (c *Client) HeadBucket(ctx context.Context) error {
	_, err := c.api.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(c.bucket),
	})
	return err
}

func (c *Client) ObjectExists(ctx context.Context, key string) (bool, error) {
	fullKey := c.FullKey(key)

	_, err := c.api.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(fullKey),
	})
	if err == nil {
		return true, nil
	}

	var respErr *smithyhttp.ResponseError
	if errors.As(err, &respErr) {
		switch respErr.HTTPStatusCode() {
		case 404, 403:
			return false, nil
		}
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		if code == "NotFound" || code == "NoSuchKey" {
			return false, nil
		}
	}

	return false, err
}

func (c *Client) UploadFile(ctx context.Context, key, filePath, contentType string) error {
	fullKey := c.FullKey(key)

	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	_, err = c.api.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(fullKey),
		Body:        f,
		ContentType: aws.String(contentType),
	})
	return err
}

func (c *Client) UploadReader(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	fullKey := c.FullKey(key)

	_, err := c.api.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(fullKey),
		Body:          r,
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(size),
	})
	return err
}

func (c *Client) UploadBytes(ctx context.Context, key string, data []byte, contentType string) error {
	fullKey := c.FullKey(key)

	_, err := c.api.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(fullKey),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	return err
}

func (c *Client) ReadFullKeyBytes(ctx context.Context, fullKey string) ([]byte, error) {
	out, err := c.api.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(fullKey),
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = out.Body.Close() }()

	return io.ReadAll(out.Body)
}

func (c *Client) ReadRelativeKeyBytes(ctx context.Context, key string) ([]byte, error) {
	return c.ReadFullKeyBytes(ctx, c.FullKey(key))
}

func (c *Client) ListUsedBytes(ctx context.Context) (int64, error) {
	infos, err := c.ListObjectsByPrefix(ctx, "")
	if err != nil {
		return 0, err
	}

	var total int64
	for _, obj := range infos {
		total += obj.Size
	}

	return total, nil
}

func (c *Client) ListObjectsByPrefix(ctx context.Context, relativePrefix string) ([]ObjectInfo, error) {
	fullPrefix := c.PrefixKey(relativePrefix)
	var objects []ObjectInfo

	paginator := s3.NewListObjectsV2Paginator(c.api, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(fullPrefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			if key == "" {
				continue
			}

			var size int64
			if obj.Size != nil {
				size = *obj.Size
			}

			objects = append(objects, ObjectInfo{
				Key:  key,
				Size: size,
			})
		}
	}

	return objects, nil
}

func (c *Client) ListFullKeysByPrefix(ctx context.Context, relativePrefix string) ([]string, error) {
	objects, err := c.ListObjectsByPrefix(ctx, relativePrefix)
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(objects))
	for _, obj := range objects {
		keys = append(keys, obj.Key)
	}

	return keys, nil
}

func (c *Client) DeleteFullKeys(ctx context.Context, fullKeys []string) error {
	if len(fullKeys) == 0 {
		return nil
	}

	const batchSize = 1000

	for start := 0; start < len(fullKeys); start += batchSize {
		end := start + batchSize
		if end > len(fullKeys) {
			end = len(fullKeys)
		}

		objects := make([]types.ObjectIdentifier, 0, end-start)
		for _, key := range fullKeys[start:end] {
			objects = append(objects, types.ObjectIdentifier{
				Key: aws.String(key),
			})
		}

		_, err := c.api.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(c.bucket),
			Delete: &types.Delete{
				Objects: objects,
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) FullKey(key string) string {
	key = strings.TrimLeft(key, "/")
	if c.prefix == "" {
		return key
	}
	return path.Join(c.prefix, key)
}

func (c *Client) PrefixKey(relativePrefix string) string {
	relativePrefix = strings.Trim(relativePrefix, "/")

	switch {
	case c.prefix == "" && relativePrefix == "":
		return ""
	case c.prefix == "" && relativePrefix != "":
		return relativePrefix + "/"
	case c.prefix != "" && relativePrefix == "":
		return c.prefix + "/"
	default:
		return path.Join(c.prefix, relativePrefix) + "/"
	}
}
