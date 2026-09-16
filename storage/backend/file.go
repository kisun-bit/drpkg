package backend

import (
	"errors"
	"io"
	"os"
	"time"
)

// file 是 StorageFile 的底层随机访问文件抽象。
//
// 不同介质提供不同的实现：
//   - fs 类读/写：直接使用 *os.File；
//   - S3 写：uploadFile（缓冲临时文件，Close 时上传）；
//   - S3 读：s3ReadFile（包装 minio.Object 的 Range 流）。
type file interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer
	Stat() (os.FileInfo, error)
	Sync() error
}

// StorageFile 是存储介质上的统一文件访问对象。
//
// 对文件系统类存储，底层直接是本地文件句柄；对 S3，写入时缓冲到本地临时
// 文件并在 Close 时上传，读取时通过 Range 流随机访问远端对象。两者的
// Read/Write/Seek/Stat/Sync 语义一致。
type StorageFile struct {
	name string
	f    file
}

// Name 返回文件路径（或 S3 对象 Key）。
func (f *StorageFile) Name() string { return f.name }

// Read 实现 io.Reader。
func (f *StorageFile) Read(p []byte) (int, error) { return f.f.Read(p) }

// Write 实现 io.Writer。
func (f *StorageFile) Write(p []byte) (int, error) { return f.f.Write(p) }

// Seek 实现 io.Seeker。
func (f *StorageFile) Seek(offset int64, whence int) (int64, error) {
	return f.f.Seek(offset, whence)
}

// Stat 返回文件的元数据。
func (f *StorageFile) Stat() (os.FileInfo, error) { return f.f.Stat() }

// Sync 持久化当前写入的内容。
func (f *StorageFile) Sync() error { return f.f.Sync() }

// Close 关闭文件；对 S3 写入，Close 会把缓冲内容提交为对象。
func (f *StorageFile) Close() error { return f.f.Close() }

// errReadOnlyFile 表示对只读文件执行写操作。
var errReadOnlyFile = errors.New("backend: file is read-only")

// objectFileInfo 把对象大小与修改时间适配为 os.FileInfo。
type objectFileInfo struct {
	size    int64
	modTime time.Time
}

func (i objectFileInfo) Name() string       { return "" }
func (i objectFileInfo) Size() int64        { return i.size }
func (i objectFileInfo) Mode() os.FileMode  { return 0 }
func (i objectFileInfo) ModTime() time.Time { return i.modTime }
func (i objectFileInfo) IsDir() bool        { return false }
func (i objectFileInfo) Sys() interface{}   { return nil }
