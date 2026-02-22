package storage

import (
	"fmt"
	"io"
)

// Mock implementation for LOCAL (if needed) or when storage is Nop
type NopStorageService struct{}

func (s *NopStorageService) UploadFile(path string, content io.Reader) error { return nil }
func (s *NopStorageService) DownloadFile(path string) (io.ReadCloser, error) { return nil, nil }

func (s *NopStorageService) SearchModule(org, module, provider, version, source, vcsType, accessToken, tagPrefix, folder string) (string, error) {
	return "", fmt.Errorf("SearchModule not supported in NopStorageService")
}

func (s *NopStorageService) DownloadModule(org, module, provider, version string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("DownloadModule not supported in NopStorageService")
}
