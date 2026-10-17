package file

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/disintegration/imaging"
	"github.com/dsoprea/go-exif/v3"
	exifcommon "github.com/dsoprea/go-exif/v3/common"
)

var thumbnailSlots = make(chan struct{}, 2)
var thumbnailCache = struct {
	sync.Mutex
	entries map[string][]byte
	bytes   int
}{entries: map[string][]byte{}}

func GetImage(path string, width, height int) ([]byte, error) {
	return GetImageContext(context.Background(), path, width, height)
}
func GetImageContext(ctx context.Context, path string, width, height int) ([]byte, error) {
	if _, err := ValidateUserPath(path); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("thumbnail requires a regular file")
	}
	key := fmt.Sprintf("%s:%d:%d:%d:%d", path, info.Size(), info.ModTime().UnixNano(), width, height)
	thumbnailCache.Lock()
	cached := thumbnailCache.entries[key]
	thumbnailCache.Unlock()
	if cached != nil {
		return append([]byte(nil), cached...), nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := GetThumbnailByOwnerPhotos(path)
	if err != nil {
		data, err = getThumbnailByWebPhoto(ctx, path, width, height)
	}
	if err != nil {
		return nil, err
	}
	if len(data) <= 1<<20 {
		thumbnailCache.Lock()
		if len(thumbnailCache.entries) >= 128 || thumbnailCache.bytes+len(data) > 16<<20 {
			thumbnailCache.entries = map[string][]byte{}
			thumbnailCache.bytes = 0
		}
		if _, ok := thumbnailCache.entries[key]; !ok {
			thumbnailCache.entries[key] = append([]byte(nil), data...)
			thumbnailCache.bytes += len(data)
		}
		thumbnailCache.Unlock()
	}
	return data, nil
}

func GetThumbnailByOwnerPhotos(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	buff := &bytes.Buffer{}

	defer file.Close()
	offset := 0
	offsets := []int{12, 30}

	head := make([]byte, 0xffff)

	r := io.TeeReader(file, buff)
	_, err = r.Read(head)
	if err != nil {
		return nil, err
	}

	for _, offset = range offsets {
		if _, err = exif.ParseExifHeader(head[offset:]); err == nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	im, err := exifcommon.NewIfdMappingWithStandard()
	if err != nil {
		return nil, err
	}

	_, index, err := exif.Collect(im, exif.NewTagIndex(), head[offset:])
	if err != nil {
		return nil, err
	}
	if index.RootIfd == nil {
		return nil, exif.ErrNoThumbnail
	}

	ifd := index.RootIfd.NextIfd()
	if ifd == nil {
		return nil, exif.ErrNoThumbnail
	}
	thumbnail, err := ifd.Thumbnail()
	if err != nil {
		return nil, err
	}
	return thumbnail, nil
}
func GetThumbnailByWebPhoto(path string, width, height int) ([]byte, error) {
	return getThumbnailByWebPhoto(context.Background(), path, width, height)
}
func getThumbnailByWebPhoto(ctx context.Context, path string, width, height int) ([]byte, error) {
	if width < 0 || height < 0 || width > 2048 || height > 2048 || width == 0 && height == 0 {
		return nil, errors.New("invalid thumbnail dimensions")
	}
	if err := safefs.NoLinks(path); err != nil {
		return nil, err
	}
	select {
	case thumbnailSlots <- struct{}{}:
		defer func() { <-thumbnailSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	input, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("thumbnail requires a regular file")
	}
	config, _, err := image.DecodeConfig(input)
	if err != nil {
		return nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 12000 || config.Height > 12000 || int64(config.Width)*int64(config.Height) > 32000000 {
		return nil, errors.New("image exceeds thumbnail decode limits")
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	src, err := imaging.Decode(input)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if width == 0 {
		width = 2048
	}
	if height == 0 {
		height = 2048
	}
	scale := math.Min(float64(width)/float64(config.Width), float64(height)/float64(config.Height))
	img := imaging.Resize(src, int(math.Max(1, float64(config.Width)*scale)), int(math.Max(1, float64(config.Height)*scale)), imaging.Linear)
	var buf bytes.Buffer
	format, err := imaging.FormatFromFilename(path)
	if err != nil {
		format = imaging.PNG
	}
	if err := imaging.Encode(&buf, img, format); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func ImageExtArray() []string {

	ext := []string{
		"ase",
		"art",
		"bmp",
		"blp",
		"cd5",
		"cit",
		"cpt",
		"cr2",
		"cut",
		"dds",
		"dib",
		"djvu",
		"egt",
		"exif",
		"gif",
		"gpl",
		"grf",
		"icns",
		"ico",
		"iff",
		"jng",
		"jpeg",
		"jpg",
		"jfif",
		"jp2",
		"jps",
		"lbm",
		"max",
		"miff",
		"mng",
		"msp",
		"nitf",
		"ota",
		"pbm",
		"pc1",
		"pc2",
		"pc3",
		"pcf",
		"pcx",
		"pdn",
		"pgm",
		"PI1",
		"PI2",
		"PI3",
		"pict",
		"pct",
		"pnm",
		"pns",
		"ppm",
		"psb",
		"psd",
		"pdd",
		"psp",
		"px",
		"pxm",
		"pxr",
		"qfx",
		"raw",
		"rle",
		"sct",
		"sgi",
		"rgb",
		"int",
		"bw",
		"tga",
		"tiff",
		"tif",
		"vtf",
		"xbm",
		"xcf",
		"xpm",
		"3dv",
		"amf",
		"ai",
		"awg",
		"cgm",
		"cdr",
		"cmx",
		"dxf",
		"e2d",
		"egt",
		"eps",
		"fs",
		"gbr",
		"odg",
		"svg",
		"stl",
		"vrml",
		"x3d",
		"sxd",
		"v2d",
		"vnd",
		"wmf",
		"emf",
		"art",
		"xar",
		"png",
		"webp",
		"jxr",
		"hdp",
		"wdp",
		"cur",
		"ecw",
		"iff",
		"lbm",
		"liff",
		"nrrd",
		"pam",
		"pcx",
		"pgf",
		"sgi",
		"rgb",
		"rgba",
		"bw",
		"int",
		"inta",
		"sid",
		"ras",
		"sun",
		"tga",
	}

	return ext
}

/**
* @description:get a image's ext
* @param {string} path "file path"
* @return {string} ext "file ext"
* @return {error} err "error info"
 */
func GetImageExt(p string) (string, error) {
	file, err := os.Open(p)
	if err != nil {
		return "", err
	}

	defer file.Close()
	buff := make([]byte, 512)

	_, err = file.Read(buff)

	if err != nil {
		return "", err
	}

	filetype := http.DetectContentType(buff)

	ext := ImageExtArray()

	for i := 0; i < len(ext); i++ {
		if strings.Contains(ext[i], filetype[6:]) {
			return ext[i], nil
		}
	}

	return "", errors.New("invalid image type")
}

func GetImageExtByName(p string) (string, error) {

	extArr := ImageExtArray()
	ext := filepath.Ext(p)
	for i := 0; i < len(extArr); i++ {
		if strings.Contains(ext, extArr[i]) {
			return extArr[i], nil
		}
	}
	return "", errors.New("invalid image type")
}
