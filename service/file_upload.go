package service

import (
	"fmt"
	"github.com/labstack/echo/v4"
	"mime/multipart"
)

// FileUploadService retains source compatibility for the retired V2 API.
// All uploads must use the validated V1 path and atomic chunk publisher.
type FileUploadService struct{}

func NewFileUploadService() *FileUploadService { return &FileUploadService{} }
func (s *FileUploadService) TestChunk(c echo.Context, identifier string, chunkNumber int64) error {
	return fmt.Errorf("legacy V2 uploads are disabled; use /v1/file/upload")
}
func (s *FileUploadService) UploadFile(c echo.Context, path string, chunkNumber, chunkSize, currentChunkSize, totalChunks, totalSize int64, identifier, relativePath, fileName string, bin *multipart.FileHeader) error {
	return fmt.Errorf("legacy V2 uploads are disabled; use /v1/file/upload")
}
