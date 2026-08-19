package storage

import (
	"context"
	"fmt"
	"io"
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

// PutObject 服务端直写对象(漫画 zip 入库用, 不经过 my_storage)。
func (m *Minio) PutObject(ctx context.Context, bucket, key, contentType string, body io.Reader, size int64) error {
	if bucket == "" {
		bucket = m.bucket
	}
	key = strings.TrimLeft(key, "/")
	if key == "" {
		return fmt.Errorf("empty object key")
	}
	if err := m.EnsureBucket(ctx, bucket); err != nil {
		return err
	}
	opts := miniogo.PutObjectOptions{}
	if contentType != "" {
		opts.ContentType = contentType
	}
	_, err := m.client.PutObject(ctx, bucket, key, body, size, opts)
	return err
}

// PresignGet 生成 GET 预签名, 供总后台预览私有桶图片。
func (m *Minio) PresignGet(ctx context.Context, bucket, key string) (string, error) {
	if bucket == "" {
		bucket = m.bucket
	}
	key = strings.TrimLeft(key, "/")
	if key == "" {
		return "", fmt.Errorf("empty object key")
	}
	u, err := m.client.PresignedGetObject(ctx, bucket, key, m.expire, nil)
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

// RemoveObject 删除单个对象；不存在视为成功。
func (m *Minio) RemoveObject(ctx context.Context, bucket, key string) error {
	if bucket == "" {
		bucket = m.bucket
	}
	key = strings.TrimLeft(key, "/")
	if key == "" {
		return nil
	}
	err := m.client.RemoveObject(ctx, bucket, key, miniogo.RemoveObjectOptions{})
	if err != nil && miniogo.ToErrorResponse(err).Code == "NoSuchKey" {
		return nil
	}
	return err
}

// RemovePrefix 删除前缀下全部对象（视频 media/source/{code}/、media/hls/{code}/；漫画 comics/{code}/）。
// 注意：minio-go 的 RemoveObjects 只回传失败，成功不会进 channel；计数需用 RemoveObjectsWithResult。
func (m *Minio) RemovePrefix(ctx context.Context, bucket, prefix string) (int, error) {
	if bucket == "" {
		bucket = m.bucket
	}
	prefix = strings.TrimLeft(prefix, "/")
	if prefix == "" {
		return 0, fmt.Errorf("empty prefix")
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	var keys []string
	for obj := range m.client.ListObjects(ctx, bucket, miniogo.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			return 0, obj.Err
		}
		if obj.Key == "" {
			continue
		}
		keys = append(keys, obj.Key)
	}
	if len(keys) == 0 {
		return 0, nil
	}

	toRemove := make(chan miniogo.ObjectInfo, 64)
	go func() {
		defer close(toRemove)
		for _, key := range keys {
			toRemove <- miniogo.ObjectInfo{Key: key}
		}
	}()

	n := 0
	for r := range m.client.RemoveObjectsWithResult(ctx, bucket, toRemove, miniogo.RemoveObjectsOptions{}) {
		if r.Err != nil {
			g.Log().Warningf(ctx, "minio remove %s/%s: %v", bucket, r.ObjectName, r.Err)
			return n, r.Err
		}
		n++
	}
	return n, nil
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
