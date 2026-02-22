package storage

import (
	"io"
)

type StorageService interface {
	SearchModule(org, module, provider, version, source, vcsType, accessToken, tagPrefix, folder string) (string, error)
	DownloadModule(org, module, provider, version string) (io.ReadCloser, error)

	UploadFile(path string, content io.Reader) error
	DownloadFile(path string) (io.ReadCloser, error)
}
