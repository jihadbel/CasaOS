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
	"github.com/IceWhaleTech/CasaOS/internal/privatefs"
	"github.com/IceWhaleTech/CasaOS/pkg/cache"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/pkg/localauth"
	"github.com/IceWhaleTech/CasaOS/pkg/sqlite"
	"github.com/IceWhaleTech/CasaOS/pkg/storagepaths"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/file"
	"github.com/IceWhaleTech/CasaOS/route"
	v1Route "github.com/IceWhaleTech/CasaOS/route/v1"
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

	configFlag         = flag.String("c", "", "config address")
	dbFlag             = flag.String("db", "", "db path")
	versionFlag        = flag.Bool("v", false, "version")
	tlsCertFlag        = flag.String("tls-cert", "", "HTTPS certificate PEM file")
	tlsKeyFlag         = flag.String("tls-key", "", "HTTPS private key PEM file")
	standaloneFlag     = flag.Bool("standalone", runtime.GOOS == "windows", "Use local accounts and protected storage for the standalone dashboard")
	prepareStorageFlag = flag.Bool("prepare-storage", false, "Validate and initialize protected standalone storage, then exit")
)

func initialize() *localauth.Manager {
	flag.Parse()
	if *versionFlag {
		fmt.Println("v" + common.VERSION)
		return nil
	}

	println("git commit:", commit)
	println("build date:", date)

	configPath := *configFlag
	if configPath == "" {
		configPath = config.CasaOSConfigFilePath
	}
	if *prepareStorageFlag && !*standaloneFlag {
		panic("-prepare-storage requires -standalone")
	}
	if *standaloneFlag {
		var err error
		configPath, err = filepath.Abs(configPath)
		if err != nil {
			panic(err)
		}
		if err := privatefs.PrepareConfig(configPath); err != nil {
			panic(fmt.Errorf("refuse untrusted service configuration: %w", err))
		}
	}
	config.InitSetup(configPath, _confSample)

	var auth *localauth.Manager
	if *standaloneFlag {
		config.PrivateStorageRoot = filepath.Join(filepath.Dir(config.AppInfo.UserDataPath), "files")
		defaults := storagepaths.Default()
		if err := storagepaths.Validate(&defaults); err != nil {
			panic(fmt.Errorf("invalid storage configuration: %w", err))
		}
		if file.InternalStoragePath(config.DefaultStorageRoot()) {
			panic("CASAOS_DATA_ROOT cannot use internal service storage")
		}
		var err error
		auth, err = localauth.NewWithStorage(filepath.Join(config.AppInfo.UserDataPath, "auth"), config.PrivateStorageRoot)
		if err != nil {
			panic(fmt.Errorf("initialize protected local accounts: %w", err))
		}
		if err := privatefs.ConfigureTemporaryDirectory(filepath.Join(filepath.Dir(config.AppInfo.UserDataPath), "tmp")); err != nil {
			panic(fmt.Errorf("protect temporary uploads: %w", err))
		}
		if *prepareStorageFlag {
			return auth
		}
		if err := os.MkdirAll(config.CommonInfo.RuntimePath, 0700); err != nil {
			panic(err)
		}
		if err := os.MkdirAll(config.AppInfo.LogPath, 0755); err != nil {
			panic(err)
		}
	}

	logger.LogInit(config.AppInfo.LogPath, config.AppInfo.LogSaveName, config.AppInfo.LogFileExt)
	v1Route.StartUploadCleanup(context.Background())
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
	return auth
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
	auth := initialize()
	if *versionFlag {
		return
	}
	if *prepareStorageFlag {
		fmt.Println("Protected standalone storage is ready:", config.PrivateStorageRoot)
		return
	}
	if (*tlsCertFlag == "") != (*tlsKeyFlag == "") {
		panic("both -tls-cert and -tls-key are required for HTTPS")
	}
	scheme := "http"
	if *tlsCertFlag != "" {
		scheme = "https"
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
	if err != nil && runtime.GOOS != "windows" {
		listener, err = net.Listen("tcp", net.JoinHostPort(bindHost, "0"))
	}
	if err != nil {
		panic(err)
	}
	if runtime.GOOS == "windows" {
		_, config.ServerInfo.HttpPort, _ = net.SplitHostPort(listener.Addr().String())
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
		if runtime.GOOS != "windows" && config.ServerInfo.HttpPort != "" {
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
	if err := file.CreateFileAndWriteContent(urlFilePath, scheme+"://"+listener.Addr().String()); err != nil {
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

	mainHandler := dashboardHandler(mux)

	var handler http.Handler = mainHandler
	if *standaloneFlag {
		authDir := filepath.Join(config.AppInfo.UserDataPath, "auth")
		privateRoot := filepath.Join(filepath.Dir(config.AppInfo.UserDataPath), "files")
		handler = auth.Middleware(privateFilesHandler(mainHandler))
		fmt.Printf("  Administrator username: admin\n  Private user storage: %s\n", privateRoot)
		fmt.Printf("  Initial administrator password file: %s\n", filepath.Join(authDir, "initial-password.txt"))
	}
	s := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	if runtime.GOOS == "windows" {
		_, port, _ := net.SplitHostPort(listener.Addr().String())
		fmt.Println("\n================================================================")
		fmt.Println("  CasaOS Windows Home Cloud File & Photo Server is Running!")
		fmt.Println("================================================================")
		fmt.Printf("  💻 Local Dashboard  : %s://localhost:%s\n", scheme, port)
		for _, ip := range ip_helper.GetDeviceAllIPv4() {
			fmt.Printf("  📱 Mobile / Wi-Fi   : %s://%s:%s\n", scheme, ip, port)
		}
		if *standaloneFlag {
			fmt.Println("  Private Storage    :", config.PrivateStorageRoot)
		} else {
			fmt.Println("  Default Storage    :", config.DefaultStorageRoot())
		}
		fmt.Println("================================================================")
	}

	logger.Info("CasaOS main service is listening...", zap.Any("address", listener.Addr().String()))
	if *tlsCertFlag != "" {
		err = s.ServeTLS(listener, *tlsCertFlag, *tlsKeyFlag)
	} else {
		err = s.Serve(listener)
	}
	if err != nil {
		panic(err)
	}
}

func dashboardHandler(mux http.Handler) http.Handler {
	return route.AuthenticateExtensions(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/v1") || strings.HasPrefix(p, "/v2") || strings.HasPrefix(p, "/v3") || strings.HasPrefix(p, "/doc") || strings.HasPrefix(p, "/ping") {
			mux.ServeHTTP(w, r)
			return
		}
		if p == "/api/zip" {
			handleZip(w, r)
			return
		}
		if p == "/api/unzip" {
			handleUnzip(w, r)
			return
		}
		if p == "/api/trash" || strings.HasPrefix(p, "/api/trash/") {
			handleTrash(w, r)
			return
		}
		if p == "/api/media/timeline" {
			handleMediaTimeline(w, r)
			return
		}
		if p == "/api/settings" {
			handleStorageSettings(w, r)
			return
		}
		if p == "/api/server-info" {
			handleServerInfo(w, r)
			return
		}
		if p != "/" {
			http.NotFound(w, r)
			return
		}
		// Serve the dashboard from this build, independent of the working directory.
		htmlContent := []byte(_webIndexHTML)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		w.Write(htmlContent)
	}))
}
