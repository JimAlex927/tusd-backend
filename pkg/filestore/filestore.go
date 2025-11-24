// Package filestore provide a storage backend based on the local file system.
//
// FileStore is a storage backend used as a handler.DataStore in handler.NewHandler.
// It stores the uploads in a directory specified in two different files: The
// `[id].info` files are used to store the fileinfo in JSON format. The
// `[id]` files without an extension contain the raw binary data uploaded.
// No cleanup is performed so you may want to run a cronjob to ensure your disk
// is not filled up with old and finished uploads.
//
// Related to the filestore is the package filelocker, which provides a file-based
// locking mechanism. The use of some locking method is recommended and further
// explained in https://tus.github.io/tusd/advanced-topics/locks/.
package filestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/JimAlex927/tusd-backend/internal/uid"
	"github.com/JimAlex927/tusd-backend/pkg/handler"
)

var defaultFilePerm = os.FileMode(0664)
var defaultDirectoryPerm = os.FileMode(0754)

const (
	// StorageKeyPath is the key of the path of uploaded file in handler.FileInfo.Storage
	StorageKeyPath = "Path"
	// StorageKeyInfoPath is the key of the path of .info file in handler.FileInfo.Storage
	StorageKeyInfoPath = "InfoPath"
)

// See the handler.DataStore interface for documentation about the different
// methods.
type FileStore struct {
	// Relative or absolute path to store files in. FileStore does not check
	// whether the path exists, use os.MkdirAll in this case on your own.
	Path string
}

// New creates a new file based storage backend. The directory specified will
// be used as the only storage entry. This method does not check
// whether the path exists, use os.MkdirAll to ensure.
func New(path string) FileStore {
	return FileStore{path}
}

// UseIn sets this store as the core data store in the passed composer and adds
// all possible extension to it.
func (store FileStore) UseIn(composer *handler.StoreComposer) {
	composer.UseCore(store)
	composer.UseTerminater(store)
	composer.UseConcater(store)
	composer.UseLengthDeferrer(store)
	composer.UseContentServer(store)
}

func (store FileStore) NewUpload(ctx context.Context, info handler.FileInfo) (handler.Upload, error) {
	if info.ID == "" {
		info.ID = uid.Uid() //这是随机生成的 uuid
	}

	// The .info file's location can directly be deduced from the upload ID ##infoPath是 与这个文件相关的 .info文件的 绝对路径
	infoPath := store.infoPath(info.ID)
	// The binary file's location might be modified by the pre-create hook.
	var binPath string
	if info.Storage != nil && info.Storage[StorageKeyPath] != "" {
		// filepath.Join treats absolute and relative paths the same, so we must
		// handle them on our own. Absolute paths get used as-is, while relative
		// paths are joined to the storage path.
		if filepath.IsAbs(info.Storage[StorageKeyPath]) {
			binPath = info.Storage[StorageKeyPath]
		} else {
			binPath = filepath.Join(store.Path, info.Storage[StorageKeyPath])
		}
	} else {
		binPath = store.defaultBinPath(info.ID) //这里返回一个 bin 路径  也就是实际存储的 binary文件  这里 和info文件的名字区别在于有无.log
	}
	//创建的文件的存储信息
	info.Storage = map[string]string{
		"Type":             "filestore",
		StorageKeyPath:     binPath,
		StorageKeyInfoPath: infoPath,
	}
	// 创建一个bin 文件 内容为空  覆盖写入！！！
	// Create binary file with no content
	if err := createFile(binPath, nil); err != nil {
		return nil, err
	}
	//整合下 info 和path
	upload := &fileUpload{
		info:     info,
		infoPath: infoPath,
		binPath:  binPath,
	}
	//创建一个.info文件  也是覆盖模式
	// writeInfo creates the file by itself if necessary
	if err := upload.writeInfo(); err != nil {
		return nil, err
	}

	return upload, nil
}

func (store FileStore) GetUpload(ctx context.Context, id string) (handler.Upload, error) {
	infoPath := store.infoPath(id)
	data, err := os.ReadFile(infoPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Interpret os.ErrNotExist as 404 Not Found
			err = handler.ErrNotFound
		}
		return nil, err
	}
	var info handler.FileInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}

	// If the info file contains a custom path to the binary file, we use that. If not, we
	// fall back to the default value (although the Path property should always be set in recent
	// tusd versions).
	var binPath string
	if info.Storage != nil && info.Storage[StorageKeyPath] != "" {
		// No filepath.Join here because the joining already happened in NewUpload. Duplicate joining
		// with relative paths lead to incorrect paths
		binPath = info.Storage[StorageKeyPath]
	} else {
		binPath = store.defaultBinPath(info.ID)
	}

	stat, err := os.Stat(binPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Interpret os.ErrNotExist as 404 Not Found
			err = handler.ErrNotFound
		}
		return nil, err
	}

	info.Offset = stat.Size()

	return &fileUpload{
		info:     info,
		binPath:  binPath,
		infoPath: infoPath,
	}, nil
}

func (store FileStore) AsTerminatableUpload(upload handler.Upload) handler.TerminatableUpload {
	return upload.(*fileUpload)
}

func (store FileStore) AsLengthDeclarableUpload(upload handler.Upload) handler.LengthDeclarableUpload {
	return upload.(*fileUpload)
}

func (store FileStore) AsConcatableUpload(upload handler.Upload) handler.ConcatableUpload {
	return upload.(*fileUpload)
}

func (store FileStore) AsServableUpload(upload handler.Upload) handler.ServableUpload {
	return upload.(*fileUpload)
}

// defaultBinPath returns the path to the file storing the binary data, if it is
// not customized using the pre-create hook.
func (store FileStore) defaultBinPath(id string) string {
	return filepath.Join(store.Path, id)
}

// infoPath returns the path to the .info file storing the file's info.
func (store FileStore) infoPath(id string) string {
	return filepath.Join(store.Path, id+".info")
}

type fileUpload struct {
	// info stores the current information about the upload
	info handler.FileInfo
	// infoPath is the path to the .info file
	infoPath string
	// binPath is the path to the binary file (which has no extension)
	binPath string
}

func (upload *fileUpload) GetInfo(ctx context.Context) (handler.FileInfo, error) {
	return upload.info, nil
}

func (upload *fileUpload) WriteChunk(ctx context.Context, offset int64, src io.Reader) (int64, error) {
	file, err := os.OpenFile(upload.binPath, os.O_WRONLY|os.O_APPEND, defaultFilePerm)
	if err != nil {
		return 0, err
	}
	// Avoid the use of defer file.Close() here to ensure no errors are lost
	// See https://github.com/tus/tusd/issues/698.

	//todo 根据源码 实际上我们可以知道 对于一个 ID 进行分片上传，是不能够乱序上传的！！！
	//todo 但是如果想要分片上传，可以 使用 Concatenate 模式，也即是每个 分片占用 一个ID
	//最后用 isFinal 即可合并

	n, err := io.Copy(file, src)
	upload.info.Offset += n
	if err != nil {
		file.Close()
		return n, err
	}

	return n, file.Close()
}

func (upload *fileUpload) GetReader(ctx context.Context) (io.ReadCloser, error) {
	return os.Open(upload.binPath)
}

func (upload *fileUpload) Terminate(ctx context.Context) error {
	// We ignore errors indicating that the files cannot be found because we want
	// to delete them anyways. The files might be removed by a cron job for cleaning up
	// or some file might have been removed when tusd crashed during the termination.
	err := os.Remove(upload.binPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	err = os.Remove(upload.infoPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

func (upload *fileUpload) ConcatUploads(ctx context.Context, uploads []handler.Upload, deletePartFile bool) (err error) {
	file, err := os.OpenFile(upload.binPath, os.O_WRONLY|os.O_APPEND, defaultFilePerm)
	if err != nil {
		return err
	}
	defer func() {
		// Ensure that close error is propagated, if it occurs.
		// See https://github.com/tus/tusd/issues/698.
		cerr := file.Close()
		if err == nil {
			err = cerr
		}
	}()

	for _, partialUpload := range uploads {
		partFileupload, ok := partialUpload.(*fileUpload)
		if !ok {
			return errors.New("Convert to fileupload failed")
		}
		// 合并到最终文件中去
		if err := partFileupload.appendTo(file); err != nil {
			return err
		}
		//todo 这里就是要实现的逻辑 把这个partialupload文件删除
		// 2. 立即删除这个子分片（主文件 + info 文件全干掉）
		if deletePartFile {
			if err := os.Remove(partFileupload.binPath); err != nil && !os.IsNotExist(err) {
				log.Printf("警告：删除子分片主文件失败 %s: %v", partFileupload.binPath, err)
				// 不 return，继续删下一个
			}

			if err := os.Remove(partFileupload.binPath + ".info"); err != nil && !os.IsNotExist(err) {
				log.Printf("警告：删除子分片 info 文件失败 %s.info: %v", partFileupload.binPath, err)
			}

			log.Printf("成功删除子分片: %s (原文件名: %s)",
				partFileupload.binPath, partFileupload.info.MetaData["filename"],
			)
		}
	}

	return
}

func (upload *fileUpload) appendTo(file *os.File) error {
	src, err := os.Open(upload.binPath)
	if err != nil {
		return err
	}

	if _, err := io.Copy(file, src); err != nil {
		src.Close()
		return err
	}

	return src.Close()
}

func (upload *fileUpload) DeclareLength(ctx context.Context, length int64) error {
	upload.info.Size = length
	upload.info.SizeIsDeferred = false
	return upload.writeInfo()
}

// writeInfo updates the entire information. Everything will be overwritten.
func (upload *fileUpload) writeInfo() error {
	data, err := json.Marshal(upload.info)
	if err != nil {
		return err
	}
	return createFile(upload.infoPath, data)
}

func (upload *fileUpload) FinishUpload(ctx context.Context) error {
	//todo 这里是文件上传完后的一个后处理位置 采用本地存储的后处理钩子函数
	info, err := upload.GetInfo(ctx)
	if err != nil {
		return err
	}
	//infoPath := upload.infoPath
	binPath := upload.binPath

	if info.CopyToPathAfterComplete != "" {
		err = CopyFileToDirWithNewName(binPath, info.CopyToPathAfterComplete, info.MetaData["filename"])
		if err != nil {
			// 外层传递error 会自动往response里面写入错误
			return err
		}
		fmt.Sprintf("文件已被拷贝到:%s/%s", info.CopyToPathAfterComplete, info.MetaData["filename"])
	}

	return nil
}

// CopyFileToDirWithNewName 将源文件拷贝到目标目录下，并使用新的文件名
//
// 参数:
//   - srcAbsPath: 源文件的绝对路径，比如 "/home/user/file.txt"
//   - targetDirAbsPath: 目标目录的绝对路径，比如 "/backup/"
//   - newFileName: 新的文件名，比如 "new_file.txt"
//
// 返回:
//   - error: 操作失败时的具体错误，成功则返回 nil
func CopyFileToDirWithNewName(srcAbsPath, targetDirAbsPath, newFileName string) error {
	// 1. 检查源文件是否存在
	if _, err := os.Stat(srcAbsPath); os.IsNotExist(err) {
		return fmt.Errorf("源文件不存在: %s", srcAbsPath)
	}

	// 2. 检查目标目录是否存在，不存在则尝试创建（也可以选择报错不创建）
	if _, err := os.Stat(targetDirAbsPath); os.IsNotExist(err) {
		// 尝试创建目标目录（包括多级目录）
		err := os.MkdirAll(targetDirAbsPath, 0755) // 0755 是常用的目录权限
		if err != nil {
			return fmt.Errorf("无法创建目标目录 %s: %v", targetDirAbsPath, err)
		}
	} else if err != nil {
		// 其他类型的错误，比如权限不足
		return fmt.Errorf("无法访问目标目录 %s: %v", targetDirAbsPath, err)
	}

	// 3. 构造目标文件的完整路径 = 目标目录 + "/" + 新文件名
	// 使用 filepath.Join 来保证跨平台兼容性（Windows 是 \，Linux 是 /）
	targetFilePath := filepath.Join(targetDirAbsPath, newFileName)

	// 4. 打开源文件
	srcFile, err := os.Open(srcAbsPath)
	if err != nil {
		return fmt.Errorf("无法打开源文件 %s: %v", srcAbsPath, err)
	}
	defer srcFile.Close()

	// 5. 创建目标文件
	dstFile, err := os.Create(targetFilePath)
	if err != nil {
		return fmt.Errorf("无法创建目标文件 %s: %v", targetFilePath, err)
	}
	defer dstFile.Close()

	// 6. 执行文件拷贝
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return fmt.Errorf("拷贝文件内容失败: %v", err)
	}

	// 7. 可选：确保数据刷入磁盘
	err = dstFile.Sync()
	if err != nil {
		return fmt.Errorf("无法同步目标文件到磁盘: %v", err)
	}

	// 成功
	return nil
}

func (upload *fileUpload) ServeContent(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	http.ServeFile(w, r, upload.binPath)

	return nil
}

// createFile creates the file with the content. If the corresponding directory does not exist,
// it is created. If the file already exists, its content is removed.  覆盖写入
func createFile(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, defaultFilePerm)
	if err != nil {
		if os.IsNotExist(err) {
			// An upload ID containing slashes is mapped onto different directories on disk,
			// for example, `myproject/uploadA` should be put into a folder called `myproject`.
			// If we get an error indicating that a directory is missing, we try to create it.
			if err := os.MkdirAll(filepath.Dir(path), defaultDirectoryPerm); err != nil {
				return fmt.Errorf("failed to create directory for %s: %s", path, err)
			}

			// Try creating the file again.
			file, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, defaultFilePerm)
			if err != nil {
				// If that still doesn't work, error out.
				return err
			}
		} else {
			return err
		}
	}

	if content != nil {
		if _, err := file.Write(content); err != nil {
			return err
		}
	}

	return file.Close()
}
