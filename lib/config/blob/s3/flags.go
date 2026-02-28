package s3

import (
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/ccontavalli/enkit/lib/kflags"
)

// Flags holds configuration options for the S3 blob store.
type Flags struct {
	Bucket    string
	Region    string
	Prefix    string
	Endpoint  string
	PathStyle bool
	Expiry    time.Duration
	AccessKey string
	SecretKey string
	Session   string
}

// DefaultFlags returns a new Flags struct with default values.
func DefaultFlags() *Flags {
	return &Flags{Expiry: defaultExpiry}
}

// Register registers the S3 flags with the provided FlagSet.
func (f *Flags) Register(set kflags.FlagSet, prefix string) *Flags {
	set.StringVar(&f.Bucket, prefix+"blob-s3-bucket", f.Bucket, "S3 bucket name")
	set.StringVar(&f.Region, prefix+"blob-s3-region", f.Region, "S3 region (optional)")
	set.StringVar(&f.Prefix, prefix+"blob-s3-prefix", f.Prefix, "S3 key prefix (optional)")
	set.StringVar(&f.Endpoint, prefix+"blob-s3-endpoint", f.Endpoint, "S3 endpoint override (optional)")
	set.BoolVar(&f.PathStyle, prefix+"blob-s3-path-style", f.PathStyle, "Use path-style S3 URLs")
	set.DurationVar(&f.Expiry, prefix+"blob-s3-expiry", f.Expiry, "Presigned URL expiry")
	set.StringVar(&f.AccessKey, prefix+"blob-s3-access-key", f.AccessKey, "S3 access key (optional)")
	set.StringVar(&f.SecretKey, prefix+"blob-s3-secret-key", f.SecretKey, "S3 secret key (optional)")
	set.StringVar(&f.Session, prefix+"blob-s3-session", f.Session, "S3 session token (optional)")
	return f
}

// FromFlags returns an Option that applies the provided flags.
func FromFlags(flags *Flags) Option {
	return func(o *options) error {
		if flags == nil {
			return nil
		}
		if flags.Bucket != "" {
			o.bucket = flags.Bucket
		}
		if flags.Region != "" {
			o.region = flags.Region
		}
		if flags.Prefix != "" {
			o.prefix = flags.Prefix
		}
		if flags.Endpoint != "" {
			o.endpoint = flags.Endpoint
		}
		o.pathStyle = flags.PathStyle
		if flags.Expiry != 0 {
			o.expiry = flags.Expiry
		}
		if flags.AccessKey != "" || flags.SecretKey != "" || flags.Session != "" {
			o.creds = credentials.NewStaticCredentialsProvider(flags.AccessKey, flags.SecretKey, flags.Session)
		}
		return nil
	}
}
