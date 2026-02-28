// Package s3 provides a lean S3-backed blob store that returns presigned URLs.
//
// For parallel uploads/downloads, use HTTP range requests with DownloadURL and
// multipart uploads with UploadURL. Multipart uploads can be supported by
// adding helpers that return presigned URLs for UploadPart and CompleteMultipart.
package s3

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ccontavalli/enkit/lib/config/blob"
)

const (
	defaultExpiry = 15 * time.Minute
)

type Store struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
	prefix  string
	expiry  time.Duration
}

type options struct {
	bucket     string
	region     string
	prefix     string
	endpoint   string
	pathStyle  bool
	expiry     time.Duration
	creds      aws.CredentialsProvider
	httpClient *http.Client
}

type Option func(*options) error

type Options []Option

func (opts Options) Apply(target *options) error {
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(target); err != nil {
			return err
		}
	}
	return nil
}

func WithBucket(bucket string) Option {
	return func(o *options) error {
		o.bucket = bucket
		return nil
	}
}

func WithRegion(region string) Option {
	return func(o *options) error {
		o.region = region
		return nil
	}
}

func WithPrefix(prefix string) Option {
	return func(o *options) error {
		o.prefix = prefix
		return nil
	}
}

func WithEndpoint(endpoint string) Option {
	return func(o *options) error {
		o.endpoint = endpoint
		return nil
	}
}

func WithPathStyle(pathStyle bool) Option {
	return func(o *options) error {
		o.pathStyle = pathStyle
		return nil
	}
}

func WithExpiry(expiry time.Duration) Option {
	return func(o *options) error {
		o.expiry = expiry
		return nil
	}
}

func WithCredentials(accessKey, secretKey, sessionToken string) Option {
	return func(o *options) error {
		o.creds = credentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken)
		return nil
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(o *options) error {
		o.httpClient = client
		return nil
	}
}

// New creates a new S3-backed blob store.
func New(mods ...Option) (*Store, error) {
	opts := options{
		expiry: defaultExpiry,
	}
	if err := Options(mods).Apply(&opts); err != nil {
		return nil, err
	}
	if opts.bucket == "" {
		return nil, fmt.Errorf("bucket is required")
	}

	ctx := context.Background()
	loadOpts := []func(*awscfg.LoadOptions) error{}
	if opts.region != "" {
		loadOpts = append(loadOpts, awscfg.WithRegion(opts.region))
	}
	if opts.creds != nil {
		loadOpts = append(loadOpts, awscfg.WithCredentialsProvider(opts.creds))
	}
	if opts.httpClient != nil {
		loadOpts = append(loadOpts, awscfg.WithHTTPClient(opts.httpClient))
	}

	cfg, err := awscfg.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if opts.endpoint != "" {
			o.EndpointResolver = s3.EndpointResolverFromURL(opts.endpoint)
		}
		o.UsePathStyle = opts.pathStyle
	})
	presign := s3.NewPresignClient(client)

	return &Store{
		client:  client,
		presign: presign,
		bucket:  opts.bucket,
		prefix:  cleanPrefix(opts.prefix),
		expiry:  opts.expiry,
	}, nil
}

func (s *Store) List() ([]blob.Descriptor, error) {
	ctx := context.Background()
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
	}
	if s.prefix != "" {
		input.Prefix = aws.String(s.prefix)
	}

	var out []blob.Descriptor
	p := s3.NewListObjectsV2Paginator(s.client, input)
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}
			key := strings.TrimPrefix(*obj.Key, s.prefix)
			if key == "" {
				continue
			}
			out = append(out, blob.Key(key))
		}
	}
	return out, nil
}

func (s *Store) DownloadURL(desc blob.Descriptor, opts ...blob.TransferOption) (*url.URL, error) {
	key := s.objectKey(desc.Key())
	params := blob.TransferOptions(opts).Apply()

	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}
	if params.ContentType != "" {
		input.ResponseContentType = aws.String(params.ContentType)
	}
	if params.Filename != "" {
		input.ResponseContentDisposition = aws.String(contentDisposition(params.Filename))
	}

	resp, err := s.presign.PresignGetObject(context.Background(), input, func(po *s3.PresignOptions) {
		po.Expires = s.expiry
	})
	if err != nil {
		return nil, err
	}
	return url.Parse(resp.URL)
}

func (s *Store) UploadURL(desc blob.Descriptor, opts ...blob.TransferOption) (*url.URL, error) {
	key := s.objectKey(desc.Key())
	params := blob.TransferOptions(opts).Apply()

	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}
	if params.ContentType != "" {
		input.ContentType = aws.String(params.ContentType)
	}
	if params.Filename != "" {
		input.ContentDisposition = aws.String(contentDisposition(params.Filename))
	}

	resp, err := s.presign.PresignPutObject(context.Background(), input, func(po *s3.PresignOptions) {
		po.Expires = s.expiry
	})
	if err != nil {
		return nil, err
	}
	return url.Parse(resp.URL)
}

func (s *Store) Delete(desc blob.Descriptor) error {
	_, err := s.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(desc.Key())),
	})
	return err
}

func (s *Store) Close() error {
	return nil
}

func (s *Store) objectKey(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + key
}

func cleanPrefix(prefix string) string {
	p := strings.TrimSpace(prefix)
	if p == "" {
		return ""
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

func contentDisposition(filename string) string {
	name := strings.ReplaceAll(filename, "\"", "")
	return fmt.Sprintf("attachment; filename=\"%s\"", name)
}

var _ blob.Store = (*Store)(nil)
