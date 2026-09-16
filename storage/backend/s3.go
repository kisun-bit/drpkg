package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config 描述 S3 对象存储的访问信息。
type S3Config struct {
	// Endpoint S3 服务地址，如 "s3.example.com" 或 "s3.example.com:9000"。
	Endpoint string
	// EnableSSL 是否启用 SSL/TLS。
	EnableSSL bool
	// AccessKey 访问密钥。
	AccessKey string
	// SecretKey 秘密密钥。
	SecretKey string
	// Region S3 Region。
	Region string
	// AccessStyle 访问风格：auto / path / virtual-hosted。
	AccessStyle string
	// Bucket Bucket 名称。
	Bucket string
	// Prefix Bucket 下的对象前缀。
	Prefix string
	// Port 服务端口，0 表示按 EnableSSL 使用默认端口。
	Port int
}

// Type 返回 S3 介质类型。
func (S3Config) Type() Type { return StorageTypeS3 }

// s3Accessor 实现 S3 对象存储的访问能力。
//
// S3 没有文件系统式的挂载概念，Online 通过 BucketExists 验证 Bucket 可达；
// StorageFile 对 S3 的写入先缓冲到本地临时文件，Close 时提交为对象。
type s3Accessor struct {
	cfg    S3Config
	client *minio.Client

	mu     sync.Mutex
	status Status
	tmpDir string
}

func init() {
	Register(StorageTypeS3, newS3)
}

func newS3(cfg Config) (Accessor, error) {
	c, ok := cfg.(S3Config)
	if !ok {
		return nil, fmt.Errorf("backend: s3 requires S3Config, got %T", cfg)
	}
	client, err := newS3Client(c)
	if err != nil {
		return nil, err
	}
	tmpDir, err := os.MkdirTemp("", "drpkg-s3-*")
	if err != nil {
		return nil, fmt.Errorf("backend: create s3 temp dir: %w", err)
	}
	return &s3Accessor{cfg: c, client: client, status: StatusOffline, tmpDir: tmpDir}, nil
}

func (a *s3Accessor) Type() Type { return StorageTypeS3 }

func (a *s3Accessor) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

// Online 验证 Bucket 可达并将介质置为在线状态。
func (a *s3Accessor) Online() error {
	if err := a.testBucket(); err != nil {
		a.mu.Lock()
		a.status = StatusError
		a.mu.Unlock()
		return err
	}
	a.mu.Lock()
	a.status = StatusOnline
	a.mu.Unlock()
	return nil
}

// Offline 释放本地缓冲并置为离线状态。
func (a *s3Accessor) Offline() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.tmpDir != "" {
		_ = os.RemoveAll(a.tmpDir)
		a.tmpDir = ""
	}
	a.status = StatusOffline
	return nil
}

// Test 通过 Bucket 探测检测介质是否可访问。
func (a *s3Accessor) Test() error { return a.testBucket() }

func (a *s3Accessor) testBucket() error {
	ok, err := a.client.BucketExists(context.Background(), a.cfg.Bucket)
	if err != nil {
		return fmt.Errorf("backend: test s3 bucket %s: %w", a.cfg.Bucket, err)
	}
	if !ok {
		return fmt.Errorf("backend: s3 bucket does not exist: %s", a.cfg.Bucket)
	}
	return nil
}

// CreateFile 在本地临时目录创建用于上传的缓冲文件。
//
// 返回的 StorageFile 在 Close 时把内容作为对象上传到 Bucket。
func (a *s3Accessor) CreateFile(path string) (*StorageFile, error) {
	if a.cfg.Bucket == "" {
		return nil, fmt.Errorf("backend: s3 requires Bucket")
	}
	f, err := a.newTempFile()
	if err != nil {
		return nil, err
	}
	return &StorageFile{name: path, f: &uploadFile{f: f, key: path, acc: a}}, nil
}

// OpenFile 以只读方式打开已有对象，通过 Range 流支持随机访问。
func (a *s3Accessor) OpenFile(path string) (*StorageFile, error) {
	if a.cfg.Bucket == "" {
		return nil, fmt.Errorf("backend: s3 requires Bucket")
	}
	key := a.objectKey(path)

	info, err := a.client.StatObject(context.Background(), a.cfg.Bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("backend: stat s3 object %s/%s: %w", a.cfg.Bucket, key, err)
	}
	obj, err := a.client.GetObject(context.Background(), a.cfg.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("backend: open s3 object %s/%s: %w", a.cfg.Bucket, key, err)
	}
	return &StorageFile{
		name: path,
		f:    &s3ReadFile{obj: obj, size: info.Size, modTime: info.LastModified},
	}, nil
}

// newTempFile 在缓冲目录内创建临时文件，目录被 Offline 清空后会自动重建。
func (a *s3Accessor) newTempFile() (*os.File, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.tmpDir == "" {
		d, err := os.MkdirTemp("", "drpkg-s3-*")
		if err != nil {
			return nil, fmt.Errorf("backend: create s3 temp dir: %w", err)
		}
		a.tmpDir = d
	}
	return os.CreateTemp(a.tmpDir, "s3obj-*")
}

// commitObject 把缓冲临时文件的内容提交为对象，并清理临时文件。
func (a *s3Accessor) commitObject(key string, f *os.File) (err error) {
	defer func() {
		os.Remove(f.Name())
		if e := f.Close(); e != nil && err == nil {
			err = e
		}
	}()

	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	return a.uploadObject(key, f, info.Size())
}

// uploadObject 将内容作为对象上传到 Bucket。
func (a *s3Accessor) uploadObject(key string, r io.Reader, size int64) error {
	if a.cfg.Bucket == "" {
		return fmt.Errorf("backend: s3 requires Bucket")
	}
	obj := a.objectKey(key)
	_, err := a.client.PutObject(
		context.Background(),
		a.cfg.Bucket,
		obj,
		r,
		size,
		minio.PutObjectOptions{},
	)
	if err != nil {
		return fmt.Errorf("backend: upload s3 object %s/%s: %w", a.cfg.Bucket, obj, err)
	}
	return nil
}

// RemoveAll 删除指定对象及其下的子对象，语义上等价于文件系统的递归删除。
func (a *s3Accessor) RemoveAll(key string) error {
	if a.cfg.Bucket == "" {
		return fmt.Errorf("backend: s3 requires Bucket")
	}
	ctx := context.Background()
	bucket := a.cfg.Bucket
	obj := a.objectKey(key)

	// 删除精确命名的对象（若存在）。
	if err := a.client.RemoveObject(ctx, bucket, obj, minio.RemoveObjectOptions{}); err != nil {
		if !isS3NotFound(err) {
			return fmt.Errorf("backend: remove s3 object %s/%s: %w", bucket, obj, err)
		}
	}

	// 删除以 obj + "/" 为前缀的所有对象。
	prefix := strings.TrimSuffix(obj, "/") + "/"
	for info := range a.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if info.Err != nil {
			return fmt.Errorf("backend: list s3 objects prefix=%s: %w", prefix, info.Err)
		}
		if err := a.client.RemoveObject(ctx, bucket, info.Key, minio.RemoveObjectOptions{}); err != nil {
			return fmt.Errorf("backend: remove s3 object %s/%s: %w", bucket, info.Key, err)
		}
	}
	return nil
}

// newS3Client 根据配置创建 minio 客户端。
func newS3Client(c S3Config) (*minio.Client, error) {
	if c.Endpoint == "" {
		return nil, fmt.Errorf("backend: s3 requires Endpoint")
	}

	lookup := minio.BucketLookupAuto
	switch c.AccessStyle {
	case "", "auto":
		lookup = minio.BucketLookupAuto
	case "path":
		lookup = minio.BucketLookupPath
	case "virtual-hosted", "virtual":
		lookup = minio.BucketLookupDNS
	default:
		return nil, fmt.Errorf("backend: unsupported s3 access style: %q", c.AccessStyle)
	}

	client, err := minio.New(s3Endpoint(c), &minio.Options{
		Creds:        credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""),
		Secure:       c.EnableSSL,
		Region:       c.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("backend: create s3 client: %w", err)
	}
	return client, nil
}

// s3Endpoint 规范化 Endpoint 并补全端口。
func s3Endpoint(c S3Config) string {
	ep := strings.TrimPrefix(c.Endpoint, "http://")
	ep = strings.TrimPrefix(ep, "https://")
	ep = strings.TrimSuffix(ep, "/")
	if c.Port != 0 && !strings.Contains(ep, ":") {
		ep += ":" + strconv.Itoa(c.Port)
	}
	return ep
}

// objectKey 返回对象在 Bucket 内的完整 Key（含 Prefix）。
func (a *s3Accessor) objectKey(key string) string {
	if a.cfg.Prefix == "" {
		return strings.TrimPrefix(key, "/")
	}
	return strings.TrimPrefix(path.Join(a.cfg.Prefix, key), "/")
}

// isS3NotFound 判断错误是否为对象不存在。
func isS3NotFound(err error) bool {
	var e minio.ErrorResponse
	if errors.As(err, &e) {
		return e.StatusCode == http.StatusNotFound ||
			e.Code == "NoSuchKey" ||
			e.Code == "NoSuchBucket"
	}
	return false
}

// uploadFile 是 S3 写入时的缓冲文件，Close 时把内容上传为对象。
type uploadFile struct {
	f   *os.File
	key string
	acc *s3Accessor
}

func (u *uploadFile) Read(p []byte) (int, error)  { return u.f.Read(p) }
func (u *uploadFile) Write(p []byte) (int, error) { return u.f.Write(p) }
func (u *uploadFile) Seek(off int64, whence int) (int64, error) {
	return u.f.Seek(off, whence)
}
func (u *uploadFile) Stat() (os.FileInfo, error) { return u.f.Stat() }
func (u *uploadFile) Sync() error                { return u.f.Sync() }
func (u *uploadFile) Close() error               { return u.acc.commitObject(u.key, u.f) }

// s3ReadFile 是 S3 读取时的随机访问文件，包装 minio.Object 的 Range 流。
type s3ReadFile struct {
	obj     *minio.Object
	size    int64
	modTime time.Time
}

func (s *s3ReadFile) Read(p []byte) (int, error)  { return s.obj.Read(p) }
func (s *s3ReadFile) Write(p []byte) (int, error) { return 0, errReadOnlyFile }
func (s *s3ReadFile) Seek(off int64, whence int) (int64, error) {
	return s.obj.Seek(off, whence)
}
func (s *s3ReadFile) Close() error { return s.obj.Close() }
func (s *s3ReadFile) Stat() (os.FileInfo, error) {
	return objectFileInfo{size: s.size, modTime: s.modTime}, nil
}
func (s *s3ReadFile) Sync() error { return nil }
