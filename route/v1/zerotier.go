package v1

import (
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/IceWhaleTech/CasaOS-Common/utils/logger"
	"github.com/IceWhaleTech/CasaOS/common"
	"github.com/IceWhaleTech/CasaOS/pkg/utils/httper"
	"github.com/labstack/echo/v4"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

func getZerotierCredFiles() (string, string) {
	if runtime.GOOS == "windows" {
		programData := os.Getenv("ProgramData")
		if len(programData) == 0 {
			programData = `C:\ProgramData`
		}
		return filepath.Join(programData, "ZeroTier", "One", "zerotier-one.port"),
			filepath.Join(programData, "ZeroTier", "One", "authtoken.secret")
	}
	return "/var/lib/zerotier-one/zerotier-one.port", "/var/lib/zerotier-one/authtoken.secret"
}

func ZerotierProxy(ctx echo.Context) error {
	portFile, authFile := getZerotierCredFiles()
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return zerotierProxy(ctx, portFile, authFile, client)
}

func zerotierProxy(ctx echo.Context, portFile, authFile string, client *http.Client) error {
	w, r := ctx.Response(), ctx.Request()
	port, err := ioutil.ReadFile(portFile)
	if err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "ZeroTier is unavailable").SetInternal(err)
	}
	number, err := strconv.Atoi(strings.TrimSpace(string(port)))
	if err != nil || number < 1 || number > 65535 {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Invalid ZeroTier port")
	}
	authToken, err := ioutil.ReadFile(authFile)
	if err != nil || strings.TrimSpace(string(authToken)) == "" {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "ZeroTier credentials are unavailable").SetInternal(err)
	}
	target := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(number)), Path: strings.TrimPrefix(r.URL.Path, "/v1/zt"), RawQuery: r.URL.RawQuery}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Unable to create ZeroTier request").SetInternal(err)
	}
	copyHeaders(req.Header, r.Header)
	req.Header.Set("X-ZT1-AUTH", strings.TrimSpace(string(authToken)))
	resp, err := client.Do(req)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "ZeroTier did not respond").SetInternal(err)
	}
	defer resp.Body.Close()
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, err = io.Copy(w, resp.Body)
	return err
}

func copyHeaders(destination, source http.Header) {
	blocked := map[string]bool{
		"Connection": true, "Proxy-Connection": true, "Keep-Alive": true,
		"Proxy-Authenticate": true, "Proxy-Authorization": true, "Te": true,
		"Trailer": true, "Transfer-Encoding": true, "Upgrade": true,
		"Content-Length": true,
	}
	for _, value := range source.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			blocked[http.CanonicalHeaderKey(strings.TrimSpace(token))] = true
		}
	}
	for key, values := range source {
		if blocked[http.CanonicalHeaderKey(key)] {
			continue
		}
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

func CheckNetwork() {
	logger.Info("start check network")
	respBody, err := httper.ZTGet("/controller/network")
	if err != nil {
		logger.Error("get network error", zap.Error(err))
		return
	}
	networkId := ""
	address := ""
	networkNames := gjson.ParseBytes(respBody).Array()
	routers := ""
	for _, v := range networkNames {
		res, err := httper.ZTGet("/controller/network/" + v.Str)
		if err != nil {
			logger.Error("get network error", zap.Error(err))
			return
		}
		name := gjson.GetBytes(res, "name").Str
		if name == common.RANW_NAME {
			networkId = gjson.GetBytes(res, "id").Str
			routers = gjson.GetBytes(res, "routes.0.target").Str
			break
		}
	}
	ip, s, e, c := getZTIP(routers)
	logger.Info("ip", zap.Any("ip", ip))
	if len(networkId) == 0 {
		if len(address) == 0 {
			address = GetAddress()
		}
		networkId = CreateNet(address, s, e, c)
	}
	res, err := httper.ZTGet("/network")
	if err != nil {
		logger.Error("get network error", zap.Error(err))
		return
	}
	joined := false
	networks := gjson.GetBytes(res, "#.id").Array()
	for _, v := range networks {
		if v.Str == networkId {
			joined = true
			break
		}
	}
	logger.Info("joined", zap.Any("joined", joined))
	if !joined {
		JoinAndUpdateNet(address, networkId, ip)
	}
}

func GetAddress() string {
	nodeRes, err := httper.ZTGet("/status")
	if err != nil {
		logger.Error("get status error", zap.Error(err))
		return ""
	}
	return gjson.GetBytes(nodeRes, "address").String()
}

func JoinAndUpdateNet(address, networkId, ip string) {
	logger.Info("start join network", zap.Any("ip", ip))
	_, err := httper.ZTPost("/network/"+networkId, "")
	if err != nil {
		logger.Error(" get network error", zap.Error(err))
		return
	}

	if len(address) == 0 {
		address = GetAddress()
	}
	b := `{
		"authorized": true,
		"activeBridge": true,
		"ipAssignments": [
		  "` + ip + `"
		]
	  }`
	_, err = httper.ZTPost("/controller/network/"+networkId+"/member/"+address, b)
	if err != nil {
		logger.Error("join network error", zap.Error(err))
		return
	}
}

func CreateNet(address, s, e, c string) string {
	body := `{
		"name": "` + common.RANW_NAME + `",
		"private": false,
		"v4AssignMode": {
		"zt": true
		},
		"ipAssignmentPools": [
		{
		"ipRangeStart": "` + s + `",
		"ipRangeEnd": "` + e + `"
		}
		],
		"routes": [
		{
		"target": "` + c + `"
		}
		],
		"rules": [
		{
		"etherType": 2048,
		"not": true,
		"or": false,
		"type": "MATCH_ETHERTYPE"
		},
		{
		"etherType": 2054,
		"not": true,
		"or": false,
		"type": "MATCH_ETHERTYPE"
		},
		{
		"etherType": 34525,
		"not": true,
		"or": false,
		"type": "MATCH_ETHERTYPE"
		},
		{
		"type": "ACTION_DROP"
		},
		{
		"type": "ACTION_ACCEPT"
		}
		],
		"v6AssignMode": {
			"rfc4193": true
		   }
		}`
	createRes, err := httper.ZTPost("/controller/network/"+address+"______", body)
	if err != nil {
		logger.Error("post network error", zap.Error(err))
		return ""
	}
	return gjson.GetBytes(createRes, "id").Str
}

func GetZTIPs() []gjson.Result {
	res, err := httper.ZTGet("/network")
	if err != nil {
		logger.Error("get network error", zap.Error(err))
		return []gjson.Result{}
	}
	a := gjson.GetBytes(res, "#.routes.0.target")
	return a.Array()
}

func getZTIP(routes string) (ip, start, end, cidr string) {
	excluded := GetZTIPs()
	cidrs := []string{
		"10.147.11.0/24",
		"10.147.12.0/24",
		"10.147.13.0/24",
		"10.147.14.0/24",
		"10.147.15.0/24",
		"10.147.16.0/24",
		"10.147.17.0/24",
		"10.147.18.0/24",
		"10.147.19.0/24",
		"10.147.20.0/24",
		"10.240.0.0/16",
		"10.241.0.0/16",
		"10.242.0.0/16",
		"10.243.0.0/16",
		"10.244.0.0/16",
		"10.245.0.0/16",
		"10.246.0.0/16",
		"10.247.0.0/16",
		"10.248.0.0/16",
		"10.249.0.0/16",
		"172.21.0.0/16",
		"172.22.0.0/16",
		"172.23.0.0/16",
		"172.24.0.0/16",
		"172.25.0.0/16",
		"172.26.0.0/16",
		"172.27.0.0/16",
		"172.28.0.0/16",
		"172.29.0.0/16",
		"172.30.0.0/16",
	}
	filteredCidrs := make([]string, 0)
	if len(routes) > 0 {
		filteredCidrs = append(filteredCidrs, routes)
	} else {
		for _, cidr := range cidrs {
			isExcluded := false
			for _, excludedIP := range excluded {
				if cidr == excludedIP.Str {
					isExcluded = true
					break
				}
			}
			if !isExcluded {
				filteredCidrs = append(filteredCidrs, cidr)
			}
		}
	}

	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	ip = ""
	if len(filteredCidrs) > 0 {
		randomIndex := rnd.Intn(len(filteredCidrs))
		selectedCIDR := filteredCidrs[randomIndex]
		_, ipNet, err := net.ParseCIDR(selectedCIDR)
		if err != nil {
			logger.Error("ParseCIDR error", zap.Error(err))
			return
		}
		cidr = selectedCIDR
		startIP := ipNet.IP
		endIP := make(net.IP, len(startIP))
		copy(endIP, startIP)

		for i := range startIP {
			endIP[i] |= ^ipNet.Mask[i]
		}
		startIP[3] = 1
		start = startIP.String()
		endIP[3] = 254
		end = endIP.String()
		ipt := ipNet
		ipt.IP[3] = 1
		ip = ipt.IP.String()
		return
	} else {
		logger.Error("No available CIDR found")
	}
	return
}
