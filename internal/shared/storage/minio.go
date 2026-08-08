package storage

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	miniogo "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Minio 对象存储(上传预签名 / 探测 / 公开 URL)。
type Minio struct {
	client     *miniogo.Client
	endpoint   string
	bucket     string
	useSSL     bool
	publicBase string
	expire     time.Duration
}

func NewMinio(ctx context.Context) (*Minio, error) {
	endpoint := g.Cfg().MustGet(ctx, "minio.endpoint", "127.0.0.1:9000").String()
	accessKey := g.Cfg().MustGet(ctx, "minio.access_key", "minioadmin").String()
	secretKey := g.Cfg().MustGet(ctx, "minio.secret_key", "minioadmin").String()
	bucket := g.Cfg().MustGet(ctx, "minio.bucket", "my-media").String()
	useSSL := g.Cfg().MustGet(ctx, "minio.use_ssl", false).Bool()
	publicBase := strings.TrimRight(g.Cfg().MustGet(ctx, "minio.public_base", "").String(), "/")
	expireSec := g.Cfg().MustGet(ctx, "minio.presign_expire_sec", 7200).Int64()
	if expireSec <= 0 {
		expireSec = 7200
	}

	cli, err := miniogo.New(endpoint, &miniogo.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio new: %w", err)
	}
	m := &Minio{
		client:     cli,
		endpoint:   endpoint,
		bucket:     bucket,
		useSSL:     useSSL,
		publicBase: publicBase,
		expire:     time.Duration(expireSec) * time.Second,
	}
	if err := m.EnsureBucket(ctx, bucket); err != nil {
		g.Log().Warningf(ctx, "minio ensure bucket: %v", err)
	}
	return m, nil
}

func (m *Minio) Bucket() string { return m.bucket }

func (m *Minio) EnsureBucket(ctx context.Context, bucket string) error {
	if bucket == "" {
		bucket = m.bucket
	}
	ok, err := m.client.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return m.client.MakeBucket(ctx, bucket, miniogo.MakeBucketOptions{})
}

// PresignPut 生成 PUT 预签名 URL，供客户端直传原片。
func (m *Minio) PresignPut(ctx context.Context, bucket, key string) (string, error) {
	if bucket == "" {
		bucket = m.bucket
	}
	if err := m.EnsureBucket(ctx, bucket); err != nil {
		return "", err
	}
	u, err := m.client.PresignedPutObject(ctx, bucket, key, m.expire)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// Stat 检查对象是否存在。
func (m *Minio) Stat(ctx context.Context, bucket, key string) error {
	if bucket == "" {
		bucket = m.bucket
	}
	_, err := m.client.StatObject(ctx, bucket, key, miniogo.StatObjectOptions{})
	return err
}

// PublicURL 拼可访问地址。
func (m *Minio) PublicURL(bucket, key string) string {
	if bucket == "" {
		bucket = m.bucket
	}
	key = strings.TrimLeft(key, "/")
	base := m.publicBase
	if base == "" {
		scheme := "http"
		if m.useSSL {
			scheme = "https"
		}
		base = fmt.Sprintf("%s://%s", scheme, m.endpoint)
	}
	// public_base 若已含桶名则不再拼 bucket
	if strings.HasSuffix(base, "/"+bucket) {
		return base + "/" + key
	}
	return fmt.Sprintf("%s/%s/%s", base, bucket, key)
}

// RewritePublicHost 可选：把预签名里的 host 换成对外可访问地址(本地调试一般不用)。
func RewritePublicHost(raw, hostBase string) (string, error) {
	if hostBase == "" {
		return raw, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw, err
	}
	b, err := url.Parse(hostBase)
	if err != nil {
		return raw, err
	}
	u.Scheme = b.Scheme
	u.Host = b.Host
	return u.String(), nil
}
