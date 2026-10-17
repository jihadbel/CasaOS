package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/IceWhaleTech/CasaOS-Common/utils/logger"
	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/internal/textfile"
	"github.com/IceWhaleTech/CasaOS/model"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/robfig/cron/v3"
	"github.com/tidwall/gjson"

	"github.com/IceWhaleTech/CasaOS/pkg/utils"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/common_err"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/file"
	"github.com/IceWhaleTech/CasaOS/service"
	model2 "github.com/IceWhaleTech/CasaOS/service/model"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type ListReq struct {
	model.PageReq
	Path string `json:"path" form:"path"`
	// Refresh bool   `json:"refresh"`
}

type ObjResp struct {
	Name       string                 `json:"name"`
	Size       int64                  `json:"size"`
	IsDir      bool                   `json:"is_dir"`
	Modified   time.Time              `json:"modified"`
	Sign       string                 `json:"sign"`
	Thumb      string                 `json:"thumb"`
	Type       int                    `json:"type"`
	Path       string                 `json:"path"`
	Date       time.Time              `json:"date"`
	Extensions map[string]interface{} `json:"extensions"`
}
type FsListResp struct {
	Content  []ObjResp `json:"content"`
	Total    int64     `json:"total"`
	Readme   string    `json:"readme,omitempty"`
	Write    bool      `json:"write,omitempty"`
	Provider string    `json:"provider,omitempty"`
	Index    int       `json:"index"`
	Size     int       `json:"size"`
}

var (
	// 升级成 WebSocket 协议
	upgraderFile = websocket.Upgrader{
		// 允许CORS跨域请求
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
)

// @Summary 读取文件
// @Produce  application/json
// @Accept application/json
// @Tags file
// @Security ApiKeyAuth
// @Param path query string true "路径"
// @Success 200 {string} string "ok"
// @Router /file/read [get]
func GetFilerContent(ctx echo.Context) error {
	filePath := ctx.QueryParam("path")
	if _, err := file.ValidateUserPath(filePath); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if len(filePath) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{
			Success: common_err.INVALID_PARAMS,
			Message: common_err.GetMsg(common_err.INVALID_PARAMS),
		})
	}
	if !file.Exists(filePath) {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{
			Success: common_err.FILE_DOES_NOT_EXIST,
			Message: common_err.GetMsg(common_err.FILE_DOES_NOT_EXIST),
		})
	}
	// 文件读取任务是将文件内容读取到内存中。
	info, err := readEditorFile(filePath)
	if errors.Is(err, errEditorTooLarge) {
		return ctx.JSON(http.StatusRequestEntityTooLarge, model.Result{Success: http.StatusRequestEntityTooLarge, Message: err.Error()})
	}
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{
			Success: common_err.FILE_READ_ERROR,
			Message: common_err.GetMsg(common_err.FILE_READ_ERROR),
			Data:    err.Error(),
		})
	}
	result, _, err := textfile.Decode(info)
	if err != nil {
		return ctx.JSON(http.StatusUnsupportedMediaType, model.Result{Success: http.StatusUnsupportedMediaType, Message: err.Error()})
	}

	return ctx.JSON(common_err.SUCCESS, model.Result{
		Success: common_err.SUCCESS,
		Message: common_err.GetMsg(common_err.SUCCESS),
		Data:    result,
	})
}

func GetLocalFile(ctx echo.Context) error {
	return GetDownloadSingleFile(ctx)
}

// @Summary download
// @Produce  application/json
// @Accept application/json
// @Tags file
// @Security ApiKeyAuth
// @Param format query string false "Compression format" Enums(zip,tar,targz)
// @Param files query string true "file list eg: filename1,filename2,filename3 "
// @Success 200 {string} string "ok"
// @Router /file/download [get]
func GetDownloadFile(ctx echo.Context) error {
	roots := strings.Split(ctx.QueryParam("files"), ",")
	if len(roots) == 1 {
		if _, err := file.ValidateUserPath(roots[0]); err != nil {
			return echo.NewHTTPError(400, err.Error())
		}
		info, err := os.Stat(roots[0])
		if err != nil {
			return echo.NewHTTPError(400, err.Error())
		}
		if !info.IsDir() {
			return serveDownloadFile(ctx, roots[0], true)
		}
		if ctx.QueryParam("format") == "" || ctx.QueryParam("format") == "zip" {
			return downloadDirectory(ctx, roots[0])
		}
	}
	entries, err := file.CollectArchiveEntries(ctx.Request().Context(), roots, 10000)
	if err != nil {
		return echo.NewHTTPError(400, err.Error())
	}
	extension, archive, err := file.GetCompressionAlgorithm(ctx.QueryParam("format"))
	if err != nil {
		return echo.NewHTTPError(400, err.Error())
	}
	ctx.Response().Header().Set("Content-Type", "application/octet-stream")
	ctx.Response().Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": "CasaOS-files" + extension}))
	ctx.Response().Header().Set("X-Content-Type-Options", "nosniff")
	ctx.Response().Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	ctx.Response().Header().Set("Cache-Control", "no-store")
	if ctx.Request().Method == http.MethodHead {
		return ctx.NoContent(http.StatusOK)
	}
	if err := archive.Create(ctx.Response()); err != nil {
		return err
	}
	if err := file.WriteArchiveEntries(ctx.Request().Context(), archive, entries); err != nil {
		_ = archive.Close()
		return err
	}
	return archive.Close()
}

func GetDownloadSingleFile(ctx echo.Context) error {
	return serveDownloadFile(ctx, ctx.QueryParam("path"), false)
}

// GetDownloadAttachment shares file validation with previews but forces download.
func GetDownloadAttachment(ctx echo.Context) error {
	return serveDownloadFile(ctx, ctx.QueryParam("path"), true)
}

func serveDownloadFile(ctx echo.Context, filePath string, attachment bool) error {
	if len(filePath) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{
			Success: common_err.INVALID_PARAMS,
			Message: common_err.GetMsg(common_err.INVALID_PARAMS),
		})
	}
	if _, err := file.ValidateUserPath(filePath); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	fi, err := os.Open(filePath)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "File cannot be opened")
	}
	defer fi.Close()
	node, err := fi.Stat()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "File cannot be read")
	}
	if node.IsDir() {
		return downloadDirectory(ctx, filePath)
	}
	if !node.Mode().IsRegular() {
		return echo.NewHTTPError(http.StatusBadRequest, "Only regular files can be downloaded")
	}
	fileName := filepath.Base(filePath)
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(fileName)))
	disposition := "attachment"
	baseType, _, _ := mime.ParseMediaType(contentType)
	switch baseType {
	case "image/jpeg", "image/png", "image/gif", "image/webp", "image/bmp", "image/avif", "image/x-icon", "image/vnd.microsoft.icon", "application/pdf", "text/plain":
		disposition = "inline"
	default:
		if strings.HasPrefix(baseType, "audio/") || strings.HasPrefix(baseType, "video/") {
			disposition = "inline"
		}
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if attachment {
		disposition = "attachment"
	}
	headers := ctx.Response().Header()
	headers.Set("Content-Type", contentType)
	headers.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": fileName}))
	headers.Set("X-Content-Type-Options", "nosniff")
	if disposition == "attachment" {
		headers.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	}
	http.ServeContent(ctx.Response().Writer, ctx.Request(), fileName, node.ModTime(), fi)
	return nil
}

// @Summary 获取目录列表
// @Produce  application/json
// @Accept application/json
// @Tags file
// @Security ApiKeyAuth
// @Param path query string false "路径"
// @Success 200 {string} string "ok"
// @Router /file/dirpath [get]
func DirPath(ctx echo.Context) error {
	var req ListReq
	path := ctx.QueryParam("path")
	req.Path = path
	req.Validate()
	info, err := service.MyService.System().GetDirPath(req.Path)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	}
	shares := service.MyService.Shares().GetSharesList()
	sharesMap := make(map[string]string)
	for _, v := range shares {
		sharesMap[v.Path] = fmt.Sprint(v.ID)
	}
	// if len(info) <= (req.Page-1)*req.Size {
	// 	return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: common_err.GetMsg(common_err.INVALID_PARAMS), Data: "page out of range"})
	// 	return
	// }
	forEnd := req.Index * req.Size
	if forEnd > len(info) {
		forEnd = len(info)
	}
	for i := (req.Index - 1) * req.Size; i < forEnd; i++ {
		if v, ok := sharesMap[info[i].Path]; ok {
			ex := make(map[string]interface{})
			shareEx := make(map[string]string)
			shareEx["shared"] = "true"
			shareEx["id"] = v
			ex["share"] = shareEx
			ex["mounted"] = false
			info[i].Extensions = ex
		}
	}
	if strings.HasPrefix(req.Path, "/mnt") || strings.HasPrefix(req.Path, "/media") {
		for i := (req.Index - 1) * req.Size; i < forEnd; i++ {
			ex := info[i].Extensions
			if ex == nil {
				ex = make(map[string]interface{})
			}
			mounted := service.IsMounted(info[i].Path)
			ex["mounted"] = mounted
			info[i].Extensions = ex
		}
	}
	// Hide the files or folders in operation
	fileQueue := make(map[string]string)
	{
		for _, queued := range service.FileOperationsSnapshot() {
			vt := queued.Operation
			for _, i := range vt.Item {
				fileQueue[filepath.Join(vt.To, filepath.Base(i.From))] = i.From
			}
		}
	}

	pathList := []ObjResp{}
	for i := (req.Index - 1) * req.Size; i < forEnd; i++ {
		if info[i].Name == ".temp" && info[i].IsDir {
			continue
		}
		if _, ok := fileQueue[info[i].Path]; !ok {
			t := ObjResp{}
			t.IsDir = info[i].IsDir
			t.Name = info[i].Name
			t.Modified = info[i].Date
			t.Date = info[i].Date
			t.Size = info[i].Size
			t.Path = info[i].Path
			t.Extensions = info[i].Extensions
			pathList = append(pathList, t)

		}
	}
	flist := FsListResp{
		Content: pathList,
		Total:   int64(len(info)),
		// Readme:   "",
		// Write:    true,
		// Provider: "local",
		Index: req.Index,
		Size:  req.Size,
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: flist})
}

// @Summary rename file or dir
// @Produce  application/json
// @Accept application/json
// @Tags file
// @Security ApiKeyAuth
// @Param oldpath body string true "path of old"
// @Param newpath body string true "path of new"
// @Success 200 {string} string "ok"
// @Router /file/rename [put]
func RenamePath(ctx echo.Context) error {
	json := make(map[string]string)
	if err := ctx.Bind(&json); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid rename request")
	}
	op, oldErr := file.ValidateDeletePath(json["old_path"])
	np, newErr := file.ValidateDeletePath(json["new_path"])
	if oldErr != nil || newErr != nil {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	}
	mounted := service.IsMounted(op)
	if mounted {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.MOUNTED_DIRECTIORIES, Message: common_err.GetMsg(common_err.MOUNTED_DIRECTIORIES), Data: common_err.GetMsg(common_err.MOUNTED_DIRECTIORIES)})
	}

	success, err := service.MyService.System().RenameFile(op, np)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{Success: success, Message: err.Error()})
	}
	status := http.StatusOK
	if success != common_err.SUCCESS {
		status = http.StatusConflict
	}
	return ctx.JSON(status, model.Result{Success: success, Message: common_err.GetMsg(success)})
}

// @Summary create folder
// @Produce  application/json
// @Accept  application/json
// @Tags file
// @Security ApiKeyAuth
// @Param path body string true "path of folder"
// @Success 200 {string} string "ok"
// @Router /file/mkdir [post]
func MkdirAll(ctx echo.Context) error {
	json := make(map[string]string)
	if err := ctx.Bind(&json); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid directory request")
	}
	path, pathErr := file.ValidateUserPath(json["path"])
	if pathErr != nil {
		return echo.NewHTTPError(http.StatusBadRequest, pathErr.Error())
	}
	var code int
	if len(path) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	}
	// decodedPath, err := url.QueryUnescape(path)
	// if err != nil {
	// 	return ctx.JSON(http.StatusOK, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	// 	return
	// }
	var err error
	code, err = service.MyService.System().MkdirAll(path)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{Success: code, Message: err.Error()})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: code, Message: common_err.GetMsg(code)})
}

// @Summary create file
// @Produce  application/json
// @Accept  application/json
// @Tags file
// @Security ApiKeyAuth
// @Param path body string true "path of folder (path need to url encode)"
// @Success 200 {string} string "ok"
// @Router /file/create [post]
func PostCreateFile(ctx echo.Context) error {
	json := make(map[string]string)
	if err := ctx.Bind(&json); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid create request")
	}
	path, pathErr := file.ValidateUserPath(json["path"])
	var code int
	if pathErr != nil {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	}
	// decodedPath, err := url.QueryUnescape(path)
	// if err != nil {
	// 	return ctx.JSON(http.StatusOK, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	// 	return
	// }
	var err error
	code, err = service.MyService.System().CreateFile(path)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, model.Result{Success: code, Message: err.Error()})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: code, Message: common_err.GetMsg(code)})
}

// @Summary upload file
// @Produce  application/json
// @Accept  application/json
// @Tags file
// @Security ApiKeyAuth
// @Param path formData string false "file path"
// @Param file formData file true "file"
// @Success 200 {string} string "ok"
// @Router /file/upload [get]
func GetFileUpload(ctx echo.Context) error {
	fileName := ctx.QueryParam("filename")
	dir, err := uploadTargetDir(ctx.QueryParam("path"), fileName, ctx.QueryParam("relativePath"))
	if err != nil {
		return ctx.JSON(400, model.Result{Success: 400, Message: err.Error()})
	}
	number, numberErr := strconv.Atoi(ctx.QueryParam("chunkNumber"))
	total, totalErr := strconv.Atoi(ctx.QueryParam("totalChunks"))
	id := ctx.QueryParam("uploadId")
	if id == "" {
		id = ctx.QueryParam("identifier")
	}
	if numberErr != nil || totalErr != nil || total < 1 || total > 100000 || number < 1 || number > total {
		return ctx.JSON(400, model.Result{Success: 400, Message: "Valid chunk metadata and uploadId are required"})
	}
	tempDir := ""
	if total > 1 {
		tempDir, err = uploadChunkDir(dir, fileName, id, total, number)
		if err != nil {
			return ctx.JSON(400, model.Result{Success: 400, Message: err.Error()})
		}
		unlock := lockUpload(tempDir)
		defer unlock()
	}
	if _, err := os.Lstat(filepath.Join(dir, fileName)); err == nil {
		cleanupUploadChunks(tempDir)
		return ctx.JSON(http.StatusConflict, model.Result{Success: http.StatusConflict, Message: common_err.GetMsg(common_err.FILE_ALREADY_EXISTS)})
	} else if !os.IsNotExist(err) {
		return ctx.JSON(500, model.Result{Success: 500, Message: err.Error()})
	}
	if total == 1 {
		return ctx.NoContent(http.StatusNoContent)
	}
	part := filepath.Join(tempDir, strconv.Itoa(number))
	if err := safefs.NoLinks(part); err != nil {
		return ctx.JSON(400, model.Result{Success: 400, Message: err.Error()})
	}
	if info, err := os.Stat(part); err == nil && info.Mode().IsRegular() {
		return ctx.JSON(200, model.Result{Success: 200, Message: common_err.GetMsg(common_err.FILE_ALREADY_EXISTS)})
	} else if err != nil && !os.IsNotExist(err) {
		return ctx.JSON(500, model.Result{Success: 500, Message: err.Error()})
	}
	return ctx.NoContent(http.StatusNoContent)
}

// @Summary upload file
// @Produce  application/json
// @Accept  multipart/form-data
// @Tags file
// @Security ApiKeyAuth
// @Param path formData string false "file path"
// @Param file formData file true "file"
// @Success 200 {string} string "ok"
// @Router /file/upload [post]

func PostFileUpload(ctx echo.Context) error {
	f, fileHeader, err := ctx.Request().FormFile("file")
	if err != nil || f == nil {
		logger.Error("error when reading multipart file", zap.Error(err))
		return ctx.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: "File missing or invalid"})
	}
	defer f.Close()
	if form := ctx.Request().MultipartForm; form != nil {
		defer form.RemoveAll()
	}

	relative := ctx.FormValue("relativePath")
	fileName := ctx.FormValue("filename")
	if len(fileName) == 0 && fileHeader != nil {
		fileName = fileHeader.Filename
	}
	targetDir, err := uploadTargetDir(ctx.FormValue("path"), fileName, relative)
	if err != nil {
		return ctx.JSON(400, model.Result{Success: common_err.INVALID_PARAMS, Message: err.Error()})
	}

	if err := os.MkdirAll(targetDir, os.ModePerm); err != nil {
		logger.Error("failed to create directory for upload", zap.String("dir", targetDir), zap.Error(err))
		return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
	}

	finalFilePath := filepath.Join(targetDir, fileName)
	totalChunks, err := strconv.Atoi(utils.DefaultPostForm(ctx, "totalChunks", "0"))
	if err != nil || totalChunks < 0 || totalChunks > 100000 {
		return ctx.JSON(400, model.Result{Success: 400, Message: "Invalid totalChunks"})
	}
	chunkNumber := ctx.FormValue("chunkNumber")
	tempDir, number := "", 0
	if totalChunks > 1 {
		number, err = strconv.Atoi(chunkNumber)
		if err != nil || number < 1 || number > totalChunks || totalChunks > 100000 {
			return ctx.JSON(400, model.Result{Success: 400, Message: "Invalid upload chunk"})
		}
		id := ctx.FormValue("uploadId")
		if id == "" {
			id = ctx.FormValue("identifier")
		}
		tempDir, err = uploadChunkDir(targetDir, fileName, id, totalChunks, number)
		if err != nil {
			return ctx.JSON(400, model.Result{Success: 400, Message: err.Error()})
		}
		unlock := lockUpload(tempDir)
		defer unlock()
		registerUploadDirectory(targetDir)
	}
	if _, err := os.Lstat(finalFilePath); err == nil {
		cleanupUploadChunks(tempDir)
		return ctx.JSON(409, model.Result{Success: 409, Message: "A file with this name already exists"})
	} else if !os.IsNotExist(err) {
		return ctx.JSON(500, model.Result{Success: 500, Message: err.Error()})
	}

	if totalChunks > 1 {
		if err := os.MkdirAll(tempDir, 0700); err != nil {
			return ctx.JSON(500, model.Result{Success: 500, Message: err.Error()})
		}

		if err := writeUploadChunk(tempDir, number, file.ContextReader{Context: ctx.Request().Context(), Reader: f}); errors.Is(err, os.ErrExist) {
			return ctx.JSON(409, model.Result{Success: 409, Message: err.Error()})
		} else if err != nil {
			return ctx.JSON(500, model.Result{Success: 500, Message: err.Error()})
		}

		ready, err := uploadChunksReady(tempDir, totalChunks)
		if err != nil {
			return ctx.JSON(500, model.Result{Success: 500, Message: err.Error()})
		}
		if ready {
			staged, err := os.CreateTemp(targetDir, ".casaos-upload-*")
			if err != nil {
				return ctx.JSON(500, model.Result{Success: 500, Message: err.Error()})
			}
			defer os.Remove(staged.Name())
			defer staged.Close()
			for i := 1; i <= totalChunks; i++ {
				part, err := os.Open(filepath.Join(tempDir, strconv.Itoa(i)))
				if err != nil {
					return ctx.JSON(500, model.Result{Success: 500, Message: err.Error()})
				}
				_, copyErr := io.Copy(staged, file.ContextReader{Context: ctx.Request().Context(), Reader: part})
				closeErr := part.Close()
				if copyErr != nil {
					return ctx.JSON(500, model.Result{Success: 500, Message: copyErr.Error()})
				}
				if closeErr != nil {
					return ctx.JSON(500, model.Result{Success: 500, Message: closeErr.Error()})
				}
			}
			if err := staged.Sync(); err != nil {
				return ctx.JSON(500, model.Result{Success: 500, Message: err.Error()})
			}
			if err := staged.Close(); err != nil {
				return ctx.JSON(500, model.Result{Success: 500, Message: err.Error()})
			}
			if err := safefs.NoReplace(staged.Name(), finalFilePath); err != nil {
				logger.Error("error splicing chunks", zap.Error(err))
				if os.IsExist(err) {
					cleanupUploadChunks(tempDir)
					return ctx.JSON(http.StatusConflict, model.Result{Success: http.StatusConflict, Message: err.Error()})
				}
				return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
			}
			cleanupUploadChunks(tempDir)
		}
	} else {
		out, err := os.CreateTemp(targetDir, ".casaos-upload-*")
		if err != nil {
			logger.Error("error creating uploaded file", zap.String("path", finalFilePath), zap.Error(err))
			return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		}
		defer out.Close()
		defer os.Remove(out.Name())

		if _, err := io.Copy(out, file.ContextReader{Context: ctx.Request().Context(), Reader: f}); err != nil {
			logger.Error("error writing uploaded file", zap.String("path", finalFilePath), zap.Error(err))
			return ctx.JSON(http.StatusInternalServerError, model.Result{Success: common_err.SERVICE_ERROR, Message: err.Error()})
		}
		if err := out.Sync(); err != nil {
			return ctx.JSON(500, model.Result{Success: 500, Message: err.Error()})
		}
		if err := out.Close(); err != nil {
			return ctx.JSON(500, model.Result{Success: 500, Message: err.Error()})
		}
		if err := safefs.NoReplace(out.Name(), finalFilePath); err != nil {
			status := http.StatusInternalServerError
			if os.IsExist(err) {
				status = http.StatusConflict
			}
			return ctx.JSON(status, model.Result{Success: status, Message: err.Error()})
		}
	}

	return ctx.JSON(http.StatusOK, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// @Summary copy or move file
// @Produce  application/json
// @Accept  application/json
// @Tags file
// @Security ApiKeyAuth
// @Param body body model.FileOperate true "type:move,copy"
// @Success 200 {string} string "ok"
// @Router /file/operate [post]
func PostOperateFileOrDir(ctx echo.Context) error {
	list := model.FileOperate{}
	if err := ctx.Bind(&list); err != nil || len(list.Item) == 0 || (list.Type != "copy" && list.Type != "move") || list.To == "" {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	}

	var total int64 = 0
	for i := 0; i < len(list.Item); i++ {
		source, _, err := file.ValidateCopyTarget(list.Item[i].From, list.To)
		if err != nil {
			return ctx.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: err.Error()})
		}
		list.Item[i].From = source
		if list.Type == "move" {
			if _, err := file.ValidateDeletePath(source); err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, err.Error())
			}
		}
		list.Item[i].Finished = false
		list.Item[i].ProcessedSize = 0
		size, err := file.GetFileOrDirSizeContext(ctx.Request().Context(), source)
		if err != nil {
			return ctx.JSON(http.StatusBadRequest, model.Result{Success: common_err.INVALID_PARAMS, Message: err.Error()})
		}
		list.Item[i].Size = size
		total += size
		if list.Type == "move" {
			mounted := service.IsMounted(list.Item[i].From)
			if mounted {
				return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.MOUNTED_DIRECTIORIES, Message: common_err.GetMsg(common_err.MOUNTED_DIRECTIORIES), Data: common_err.GetMsg(common_err.MOUNTED_DIRECTIORIES)})
			}
		}
	}

	list.TotalSize = total
	list.ProcessedSize = 0
	list.Finished, list.Running, list.Error = false, false, ""

	uid := uuid.NewString()
	service.QueueFileOperation(uid, list)

	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// @Summary delete file
// @Produce  application/json
// @Accept  application/json
// @Tags file
// @Security ApiKeyAuth
// @Param body body string true "paths eg ["/a/b/c","/d/e/f"]"
// @Success 200 {string} string "ok"
// @Router /file/delete [delete]
func DeleteFile(ctx echo.Context) error {
	paths := []string{}
	if err := ctx.Bind(&paths); err != nil || len(paths) == 0 {
		return ctx.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.INVALID_PARAMS, Message: common_err.GetMsg(common_err.INVALID_PARAMS)})
	}
	for index, raw := range paths {
		clean, err := file.ValidateDeletePath(raw)
		if err != nil {
			return ctx.JSON(http.StatusBadRequest, model.Result{Success: http.StatusBadRequest, Message: err.Error()})
		}
		paths[index] = clean
	}
	for _, v := range paths {
		mounted := service.IsMounted(v)
		if mounted {
			return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.MOUNTED_DIRECTIORIES, Message: common_err.GetMsg(common_err.MOUNTED_DIRECTIORIES), Data: common_err.GetMsg(common_err.MOUNTED_DIRECTIORIES)})
		}
	}

	for _, v := range paths {
		err := os.RemoveAll(v)
		if err != nil {
			return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.FILE_DELETE_ERROR, Message: common_err.GetMsg(common_err.FILE_DELETE_ERROR), Data: err})
		}
	}

	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// @Summary update file
// @Produce  application/json
// @Accept  application/json
// @Tags file
// @Security ApiKeyAuth
// @Param path body string true "path"
// @Param content body string true "content"
// @Success 200 {string} string "ok"
// @Router /file/update [put]
func PutFileContent(ctx echo.Context) error {
	if ctx.Request().ContentLength > maxEditorRequestBytes {
		return ctx.JSON(http.StatusRequestEntityTooLarge, model.Result{Success: http.StatusRequestEntityTooLarge, Message: "Editor request is too large"})
	}
	ctx.Request().Body = http.MaxBytesReader(ctx.Response(), ctx.Request().Body, maxEditorRequestBytes)
	fi := model.FileUpdate{}
	bindErr := ctx.Bind(&fi)
	var tooLarge *http.MaxBytesError
	if errors.As(bindErr, &tooLarge) {
		return ctx.JSON(http.StatusRequestEntityTooLarge, model.Result{Success: http.StatusRequestEntityTooLarge, Message: "Editor request is too large"})
	}
	if bindErr != nil || fi.FilePath == "" {
		return ctx.JSON(400, model.Result{Success: 400, Message: "Invalid file content request"})
	}
	if _, err := file.ValidateUserPath(fi.FilePath); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if len(fi.FileContent) > 2*maxEditorFileBytes {
		return ctx.JSON(http.StatusRequestEntityTooLarge, model.Result{Success: http.StatusRequestEntityTooLarge, Message: errEditorTooLarge.Error()})
	}

	// path := ctx.FormValue("path")
	// content := ctx.FormValue("content")
	if !file.Exists(fi.FilePath) {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.FILE_ALREADY_EXISTS, Message: common_err.GetMsg(common_err.FILE_ALREADY_EXISTS)})
	}
	// err := os.Remove(path)
	f, err := os.Stat(fi.FilePath)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.FILE_ALREADY_EXISTS, Message: common_err.GetMsg(common_err.FILE_ALREADY_EXISTS)})
	}
	fm := f.Mode()
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.FILE_DELETE_ERROR, Message: common_err.GetMsg(common_err.FILE_DELETE_ERROR), Data: err})
	}
	if !f.Mode().IsRegular() {
		return ctx.JSON(400, model.Result{Success: 400, Message: "Only regular files can be edited"})
	}
	if err := safefs.NoLinks(fi.FilePath); err != nil {
		return ctx.JSON(400, model.Result{Success: 400, Message: err.Error()})
	}
	original, err := readEditorFile(fi.FilePath)
	if errors.Is(err, errEditorTooLarge) {
		return ctx.JSON(http.StatusRequestEntityTooLarge, model.Result{Success: http.StatusRequestEntityTooLarge, Message: err.Error()})
	}
	if err != nil {
		return ctx.JSON(500, model.Result{Success: 500, Message: err.Error()})
	}
	_, format, err := textfile.Decode(original)
	if err != nil {
		return ctx.JSON(415, model.Result{Success: 415, Message: err.Error()})
	}
	encoded, err := format.Encode(fi.FileContent)
	if err != nil {
		return ctx.JSON(400, model.Result{Success: 400, Message: err.Error()})
	}
	if len(encoded) > maxEditorFileBytes {
		return ctx.JSON(http.StatusRequestEntityTooLarge, model.Result{Success: http.StatusRequestEntityTooLarge, Message: errEditorTooLarge.Error()})
	}
	err = safefs.WriteAtomic(fi.FilePath, encoded, fm.Perm())
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

// @Summary image thumbnail/original image
// @Produce  application/json
// @Accept  application/json
// @Tags file
// @Security ApiKeyAuth
// @Param path query string true "path"
// @Param type query string false "original,thumbnail" Enums(original,thumbnail)
// @Success 200 {string} string "ok"
// @Router /file/image [get]
func GetFileImage(ctx echo.Context) error {
	if ctx.QueryParam("type") == "thumbnail" {
		if file.InternalStoragePath(ctx.QueryParam("path")) {
			return echo.NewHTTPError(http.StatusBadRequest, "Internal storage cannot be downloaded")
		}
		if err := safefs.NoLinks(ctx.QueryParam("path")); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Links cannot be downloaded")
		}
		data, err := file.GetImageContext(ctx.Request().Context(), ctx.QueryParam("path"), 100, 0)
		if err != nil {
			return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
		}
		contentType := http.DetectContentType(data)
		if !strings.HasPrefix(contentType, "image/") || contentType == "image/svg+xml" {
			return echo.NewHTTPError(http.StatusUnsupportedMediaType, "Invalid image thumbnail")
		}
		ctx.Response().Header().Set("X-Content-Type-Options", "nosniff")
		return ctx.Blob(http.StatusOK, contentType, data)
	}
	return GetDownloadSingleFile(ctx)
}

func DeleteOperateFileOrDir(ctx echo.Context) error {
	id := ctx.Param("id")
	service.CancelFileOperation(id)
	if service.MyService != nil {
		go service.MyService.Notify().SendFileOperateNotify(true)
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS)})
}

func GetSize(ctx echo.Context) error {
	json := make(map[string]string)
	ctx.Bind(&json)
	path := json["path"]
	size, err := file.GetFileOrDirSizeContext(ctx.Request().Context(), path)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: size})
}

func GetFileCount(ctx echo.Context) error {
	json := make(map[string]string)
	ctx.Bind(&json)
	path := json["path"]
	list, err := ioutil.ReadDir(path)
	if err != nil {
		return ctx.JSON(common_err.SERVICE_ERROR, model.Result{Success: common_err.SERVICE_ERROR, Message: common_err.GetMsg(common_err.SERVICE_ERROR), Data: err.Error()})
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: len(list)})
}

type CenterHandler struct {
	mu sync.RWMutex
	// 广播通道，有数据则循环每个用户广播出去
	broadcast chan []byte
	// 注册通道，有用户进来 则推到用户集合map中
	register chan *Client
	// 注销通道，有用户关闭连接 则将该用户剔出集合map中
	unregister chan *Client
	// 用户集合，每个用户本身也在跑两个协程，监听用户的读、写的状态
	clients map[string]*Client
}

type Client struct {
	handler *CenterHandler
	conn    *websocket.Conn
	// 每个用户自己的循环跑起来的状态监控
	send         chan []byte
	done         chan struct{}
	stopOnce     sync.Once
	ID           string       `json:"id"`
	IP           string       `json:"ip"`
	Name         service.Name `json:"name"`
	RtcSupported bool         `json:"rtcSupported"`
	TimerId      int          `json:"timerId"`
	LastBeat     time.Time    `json:"lastBeat"`
}

type PeerModel struct {
	ID           string       `json:"id"`
	Name         service.Name `json:"name"`
	RtcSupported bool         `json:"rtcSupported"`
}

func ConnectWebSocket(ctx echo.Context) error {
	peerId := ctx.QueryParam("peer")
	writer := ctx.Response().Writer
	request := ctx.Request()
	key := uuid.NewString()
	// peerModel := service.MyService.Peer().GetPeerByUserAgent(ctx.Request().UserAgent())
	peerModel := model2.PeerDriveDBModel{}
	conn, err := upgraderFile.Upgrade(writer, request, writer.Header())
	if err != nil {
		return err
	}
	name := service.GetName(request)
	client := &Client{handler: &handler, conn: conn, send: make(chan []byte, 256), done: make(chan struct{}), ID: service.GetPeerId(request, key), IP: service.GetIP(request), Name: name, RtcSupported: true, TimerId: 0, LastBeat: time.Now()}
	if peerId != "" || len(peerModel.ID) > 0 {
		if len(peerModel.ID) == 0 {
			peerModel = service.MyService.Peer().GetPeerByID(peerId)
		}
		if len(peerModel.ID) > 0 {
			key = peerId
			client.ID = peerModel.ID
			client.Name = service.GetNameByDB(peerModel)
		}
	}
	list := service.MyService.Peer().GetPeers()
	if len(peerModel.ID) == 0 {
		peerModel.ID = key
		peerModel.DisplayName = name.DisplayName
		peerModel.DeviceName = name.DeviceName
		peerModel.Model = name.Model
		peerModel.OS = name.OS
		peerModel.Browser = name.Browser
		peerModel.UserAgent = ctx.Request().UserAgent()
		peerModel.IP = client.IP
		service.MyService.Peer().CreatePeer(&peerModel)
		list = append(list, peerModel)
	}

	cookie := http.Cookie{
		Name:  "peerid",
		Value: key,
		Path:  "/",
	}
	http.SetCookie(writer, &cookie)
	if len(list) > 10 {
		kickoutList := []Client{}
		count := len(list) - 10
		for i := len(list) - 1; count > 0 && i > -1; i-- {
			if handler.lookup(list[i].ID) == nil {
				count--
				kickoutList = append(kickoutList, Client{ID: list[i].ID, Name: service.GetNameByDB(list[i]), IP: list[i].IP})
				service.MyService.Peer().DeletePeer(list[i].ID)
			}
		}
		// if len(kickoutList) > 0 {
		// 	other := make(map[string]interface{})
		// 	other["type"] = "kickout"
		// 	other["peers"] = kickoutList
		// 	otherBy, _ := json.Marshal(other)
		// 	fmt.Println(err)
		// 	client.handler.broadcast <- otherBy
		// }
	}
	list = service.MyService.Peer().GetPeers()
	if len(list) > 10 {

	}
	currentPeer := PeerModel{ID: client.ID, Name: client.Name, RtcSupported: client.RtcSupported}
	pmsg := make(map[string]interface{})
	pmsg["type"] = "peer-joined"
	pmsg["peer"] = currentPeer
	pby, _ := json.Marshal(pmsg)

	for _, v := range handler.snapshot() {
		v.enqueue(pby)
	}
	// client.handler.broadcast <- pby
	clients := []PeerModel{}
	for _, v := range client.handler.snapshot() {
		clients = append(clients, PeerModel{ID: v.ID, Name: v.Name, RtcSupported: v.RtcSupported})
	}

	other := make(map[string]interface{})
	other["type"] = "peers"
	other["peers"] = clients
	otherBy, err := json.Marshal(other)

	client.enqueue(otherBy)
	client.enqueue([]byte(`{"type":"ping"}`))

	data := make(map[string]string)
	data["displayName"] = client.Name.DisplayName
	data["deviceName"] = client.Name.DeviceName
	data["id"] = client.ID
	msg := make(map[string]interface{})
	msg["type"] = "display-name"
	msg["message"] = data
	by, _ := json.Marshal(msg)
	client.enqueue(by)
	handler.register <- client

	// 每个 client 都挂起 2 个新的协程，监控读、写状态
	go client.writePump()
	go client.readPump()
	return nil
}

func (ch *CenterHandler) lookup(id string) *Client {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	return ch.clients[id]
}

func (ch *CenterHandler) snapshot() []*Client {
	ch.mu.RLock()
	defer ch.mu.RUnlock()
	clients := make([]*Client, 0, len(ch.clients))
	for _, client := range ch.clients {
		clients = append(clients, client)
	}
	return clients
}

func (c *Client) stop() {
	c.stopOnce.Do(func() {
		close(c.done)
		c.conn.Close()
	})
}

// A stalled peer must not block registration, broadcasts or other peers.
func (c *Client) enqueue(message []byte) {
	select {
	case <-c.done:
	case c.send <- message:
	default:
		c.stop()
	}
}

var handler = CenterHandler{
	broadcast:  make(chan []byte),
	register:   make(chan *Client),
	unregister: make(chan *Client),
	clients:    make(map[string]*Client),
}

func init() {
	// 起个协程跑起来，监听注册、注销、消息 3 个 channel
	go handler.monitoring()

	crontab := cron.New(cron.WithSeconds()) // 精确到秒
	// 定义定时器调用的任务函数

	task := func() {
		handler.broadcast <- []byte(`{"type":"ping"}`)
	}
	// 定时任务
	spec := "*/30 * * * * ?" // cron表达式，每五秒一次
	// 添加定时任务,
	crontab.AddFunc(spec, task)
	// 启动定时器
	crontab.Start()
}

func (c *Client) writePump() {
	defer func() {
		c.stop()
		c.handler.unregister <- c
	}()
	for {
		// 广播推过来的新消息，马上通过websocket推给自己
		var message []byte
		select {
		case <-c.done:
			return
		case message = <-c.send:
		}
		c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

		if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			return
		}
	}
}

// 读，监听客户端是否有推送内容过来服务端
func (c *Client) readPump() {
	c.conn.SetReadLimit(64 << 10)
	defer func() {
		c.stop()
		c.handler.unregister <- c
	}()
	for {
		// 循环监听是否该用户是否要发言
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			// 异常关闭的处理
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			message, _ := json.Marshal(map[string]string{"type": "peer-left", "peerId": c.ID})
			c.handler.broadcast <- message
			break
		}
		// 要的话，推给广播中心，广播中心再推给每个用户

		t := gjson.GetBytes(message, "type")
		if t.String() == "disconnect" {
			c.handler.unregister <- c
			c.conn.Close()
			// clients := []Client{}
			// list := service.MyService.Peer().GetPeers()
			// for _, v := range list {
			// 	if _, ok := handler.clients[v.ID]; ok {
			// 		clients = append(clients, *handler.clients[v.ID])
			// 	} else {
			// 		clients = append(clients, Client{ID: v.ID, Name: service.GetNameByDB(v), IP: v.IP, Offline: true})
			// 	}
			// }
			// other := make(map[string]interface{})
			// other["type"] = "peers"
			// other["peers"] = clients
			// otherBy, err := json.Marshal(other)
			// fmt.Println(err)
			message, _ := json.Marshal(map[string]string{"type": "peer-left", "peerId": c.ID})
			c.handler.broadcast <- message
			// c.handler.broadcast <- otherBy
			break
		} else if t.String() == "pong" {
			c.LastBeat = time.Now()
			continue
		}
		to := gjson.GetBytes(message, "to")

		if len(to.String()) > 0 {
			toC := c.handler.lookup(to.String())
			if toC == nil {
				continue
			}
			data := map[string]interface{}{}
			json.Unmarshal(message, &data)
			data["sender"] = c.ID
			delete(data, "to")
			message, err = json.Marshal(data)
			toC.enqueue(message)
			continue
		}

		c.handler.broadcast <- message
	}
}

func (ch *CenterHandler) monitoring() {
	for {
		select {
		// 注册，新用户连接过来会推进注册通道，这里接收推进来的用户指针
		case client := <-ch.register:
			ch.mu.Lock()
			previous := ch.clients[client.ID]
			ch.clients[client.ID] = client
			ch.mu.Unlock()
			if previous != nil && previous != client {
				previous.stop()
			}
			// 注销，关闭连接或连接异常会将用户推出群聊
		case client := <-ch.unregister:
			ch.mu.Lock()
			if ch.clients[client.ID] == client {
				delete(ch.clients, client.ID)
			}
			ch.mu.Unlock()
			client.stop()
			// 消息，监听到有新消息到来
		case message := <-ch.broadcast:

			// 推送给每个用户的通道，每个用户都有跑协程起了writePump的监听
			for _, client := range ch.snapshot() {
				client.enqueue(message)
			}
		}
	}
}

func GetPeers(ctx echo.Context) error {
	peers := service.MyService.Peer().GetPeers()
	for i := 0; i < len(peers); i++ {
		if handler.lookup(peers[i].ID) != nil {
			peers[i].Online = true
		}
	}
	return ctx.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: peers})
}
