//go:generate bash -c "mkdir -p codegen && go run github.com/deepmap/oapi-codegen/cmd/oapi-codegen@v1.12.4 -generate types,server,spec -package codegen api/casaos/openapi.yaml > codegen/casaos_api.go"
//go:generate bash -c "mkdir -p codegen/message_bus && go run github.com/deepmap/oapi-codegen/cmd/oapi-codegen@v1.12.4 -generate types,client -package message_bus https://raw.githubusercontent.com/IceWhaleTech/CasaOS-MessageBus/main/api/message_bus/openapi.yaml > codegen/message_bus/api.go"
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/IceWhaleTech/CasaOS-Common/model"
	"github.com/IceWhaleTech/CasaOS-Common/utils/command"
	"github.com/IceWhaleTech/CasaOS-Common/utils/constants"
	"github.com/IceWhaleTech/CasaOS-Common/utils/logger"

	util_http "github.com/IceWhaleTech/CasaOS-Common/utils/http"

	"github.com/IceWhaleTech/CasaOS/common"
	"github.com/IceWhaleTech/CasaOS/pkg/cache"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/pkg/sqlite"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/file"
	"github.com/IceWhaleTech/CasaOS/route"
	"github.com/IceWhaleTech/CasaOS/service"
	"github.com/coreos/go-systemd/daemon"
	"go.uber.org/zap"

	"github.com/IceWhaleTech/CasaOS/pkg/utils/ip_helper"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
	"os"
	"runtime"
	"strings"
)

const LOCALHOST = "127.0.0.1"

var sqliteDB *gorm.DB

var (
	commit = "private build"
	date   = "private build"

	//go:embed api/index.html
	_docHTML string

	//go:embed api/casaos/openapi.yaml
	_docYAML string

	//go:embed build/sysroot/etc/casaos/casaos.conf.sample
	_confSample string

	//go:embed web/index.html
	_webIndexHTML string

	configFlag  = flag.String("c", "", "config address")
	dbFlag      = flag.String("db", "", "db path")
	versionFlag = flag.Bool("v", false, "version")
)

func init() {
	flag.Parse()
	if *versionFlag {
		fmt.Println("v" + common.VERSION)
		return
	}

	println("git commit:", commit)
	println("build date:", date)

	config.InitSetup(*configFlag, _confSample)

	if runtime.GOOS == "windows" {
		_ = os.MkdirAll(`C:\CasaOS\DATA\Photos`, os.ModePerm)
		_ = os.MkdirAll(`C:\CasaOS\DATA\Videos`, os.ModePerm)
		_ = os.MkdirAll(`C:\CasaOS\DATA\Documents`, os.ModePerm)
		_ = os.MkdirAll(`C:\CasaOS\DATA\Music`, os.ModePerm)
		_ = os.MkdirAll(`C:\CasaOS\DATA\Applications`, os.ModePerm)
		_ = os.MkdirAll(`C:\CasaOS\DATA\Archives`, os.ModePerm)
		_ = os.MkdirAll(`C:\CasaOS\DATA\Mobile_Uploads`, os.ModePerm)
		_ = os.MkdirAll(config.CommonInfo.RuntimePath, os.ModePerm)
		_ = os.MkdirAll(config.AppInfo.LogPath, os.ModePerm)
	}

	logger.LogInit(config.AppInfo.LogPath, config.AppInfo.LogSaveName, config.AppInfo.LogFileExt)
	if len(*dbFlag) == 0 {
		*dbFlag = config.AppInfo.DBPath + "/db"
	}

	sqliteDB = sqlite.GetDb(*dbFlag)
	// gredis.GetRedisConn(config.RedisInfo),

	service.MyService = service.NewService(sqliteDB, config.CommonInfo.RuntimePath)

	service.Cache = cache.Init()

	service.GetCPUThermalZone()

	route.InitFunction()

	//service.MyService.System().GenreateSystemEntry()
	///
	//service.MountLists = make(map[string]*mountlib.MountPoint)
	//configfile.Install()
}

// @title casaOS API
// @version 1.0.0
// @contact.name lauren.pan
// @contact.url https://www.zimaboard.com
// @contact.email lauren.pan@icewhale.org
// @description casaOS v1版本api
// @host 192.168.2.217:8089
// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name Authorization
// @BasePath /v1
func main() {
	if *versionFlag {
		return
	}
	v1Router := route.InitV1Router()

	v2Router := route.InitV2Router()
	v2DocRouter := route.InitV2DocRouter(_docHTML, _docYAML)
	v3File := route.InitFile()
	mux := &util_http.HandlerMultiplexer{
		HandlerMap: map[string]http.Handler{
			"v1":  v1Router,
			"v2":  v2Router,
			"v3":  v3File,
			"doc": v2DocRouter,
		},
	}

	crontab := cron.New(cron.WithSeconds())
	if _, err := crontab.AddFunc("@every 5s", route.SendAllHardwareStatusBySocket); err != nil {
		logger.Error("add crontab error", zap.Error(err))
	}

	crontab.Start()
	defer crontab.Stop()

	portToListen := "0"
	if config.ServerInfo.HttpPort != "" {
		portToListen = config.ServerInfo.HttpPort
	} else if runtime.GOOS == "windows" {
		portToListen = "8080"
	}

	bindHost := LOCALHOST
	if runtime.GOOS == "windows" {
		bindHost = "" // Allow LAN/Wi-Fi devices to connect
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(bindHost, portToListen))
	if err != nil {
		listener, err = net.Listen("tcp", net.JoinHostPort(bindHost, "0"))
		if err != nil {
			panic(err)
		}
	}
	routers := []string{
		"/v1/sys",
		"/v1/port",
		"/v1/file",
		"/v1/folder",
		"/v1/batch",
		"/v1/image",
		"/v1/samba",
		"/v1/notify",
		"/v1/driver",
		"/v1/cloud",
		"/v1/recover",
		"/v1/other",
		"/v1/zt",
		"/v1/test",
		route.V2APIPath,
		route.V2DocPath,
		route.V3FilePath,
	}
	for _, apiPath := range routers {
		err = service.MyService.Gateway().CreateRoute(&model.Route{
			Path:   apiPath,
			Target: "http://" + listener.Addr().String(),
		})
		if err != nil {
			logger.Info("gateway route registration skipped/failed", zap.String("path", apiPath), zap.Error(err))
		}
	}

	// register at message bus
	for i := 0; i < 2; i++ {
		response, err := service.MyService.MessageBus().RegisterEventTypesWithResponse(context.Background(), common.EventTypes)
		if err != nil {
			logger.Info("message bus not reachable - running in standalone mode")
			break
		}
		if response != nil && response.StatusCode() != http.StatusOK {
			logger.Info("message bus registration skipped", zap.String("status", response.Status()))
			break
		}
		if response != nil && response.StatusCode() == http.StatusOK {
			break
		}
		time.Sleep(time.Second)
	}

	go func() {
		time.Sleep(time.Second * 2)
		// v0.3.6
		if config.ServerInfo.HttpPort != "" {
			changePort := model.ChangePortRequest{}
			changePort.Port = config.ServerInfo.HttpPort
			err := service.MyService.Gateway().ChangePort(&changePort)
			if err == nil {
				config.Cfg.Section("server").Key("HttpPort").SetValue("")
				config.Cfg.SaveTo(config.SystemConfigInfo.ConfigPath)
			}
		}
	}()

	urlFilePath := filepath.Join(config.CommonInfo.RuntimePath, "casaos.url")
	if err := file.CreateFileAndWriteContent(urlFilePath, "http://"+listener.Addr().String()); err != nil {
		logger.Error("error when creating address file", zap.Error(err),
			zap.Any("address", listener.Addr().String()),
			zap.Any("filepath", urlFilePath),
		)
	}

	// run any script that needs to be executed (Linux only)
	if runtime.GOOS != "windows" {
		scriptDirectory := filepath.Join(constants.DefaultConfigPath, "start.d")
		command.ExecuteScripts(scriptDirectory)
	}

	if supported, err := daemon.SdNotify(false, daemon.SdNotifyReady); err != nil {
		logger.Error("Failed to notify systemd that casaos main service is ready", zap.Any("error", err))
	} else if supported {
		logger.Info("Notified systemd that casaos main service is ready")
	} else {
		logger.Info("This process is not running as a systemd service.")
	}
	// http.HandleFunc("/v1/file/test", func(w http.ResponseWriter, r *http.Request) {

	// 	//http.ServeFile(w, r, r.URL.Path[1:])
	// 	http.ServeFile(w, r, "/DATA/test.img")
	// })
	// go http.ListenAndServe(":8081", nil)

	mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/v1") || strings.HasPrefix(p, "/v2") || strings.HasPrefix(p, "/v3") || strings.HasPrefix(p, "/doc") || strings.HasPrefix(p, "/ping") {
			mux.ServeHTTP(w, r)
			return
		}
		// Serve Web Dashboard (try live file first for instant updates, fallback to embedded)
		var htmlContent []byte
		if content, err := os.ReadFile("web/index.html"); err == nil {
			htmlContent = content
		} else {
			htmlContent = []byte(_webIndexHTML)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(htmlContent)
	})

	s := &http.Server{
		Handler:           mainHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	if runtime.GOOS == "windows" {
		_, port, _ := net.SplitHostPort(listener.Addr().String())
		fmt.Println("\n================================================================")
		fmt.Println("  CasaOS Windows Home Cloud File & Photo Server is Running!")
		fmt.Println("================================================================")
		fmt.Printf("  💻 Local Dashboard  : http://localhost:%s\n", port)
		for _, ip := range ip_helper.GetDeviceAllIPv4() {
			fmt.Printf("  📱 Mobile / Wi-Fi   : http://%s:%s\n", ip, port)
		}
		fmt.Println("  📁 Default Storage  : C:\\CasaOS\\DATA")
		fmt.Println("================================================================\n")
	}

	logger.Info("CasaOS main service is listening...", zap.Any("address", listener.Addr().String()))
	err = s.Serve(listener)
	if err != nil {
		panic(err)
	}
}
