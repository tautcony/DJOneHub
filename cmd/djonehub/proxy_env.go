package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/iniwex5/vohive/pkg/logger"
)

type proxyEnvironmentSetting struct {
	name     string
	endpoint string
	err      error
}

func inspectProxyEnvironment() ([]proxyEnvironmentSetting, string) {
	settings := make([]proxyEnvironmentSetting, 0, 2)
	for _, names := range [][2]string{{"HTTP_PROXY", "http_proxy"}, {"HTTPS_PROXY", "https_proxy"}} {
		name, value := firstEnvironmentValue(names[0], names[1])
		if value == "" {
			continue
		}
		endpoint, err := sanitizedProxyEndpoint(value)
		settings = append(settings, proxyEnvironmentSetting{name: name, endpoint: endpoint, err: err})
	}
	noProxyName, _ := firstEnvironmentValue("NO_PROXY", "no_proxy")
	return settings, noProxyName
}

func firstEnvironmentValue(names ...string) (string, string) {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return name, value
		}
	}
	return "", ""
}

func sanitizedProxyEndpoint(value string) (string, error) {
	value = strings.TrimSpace(value)
	parsedValue := value
	if !strings.Contains(parsedValue, "://") {
		parsedValue = "http://" + parsedValue
	}
	proxyURL, err := url.Parse(parsedValue)
	if err != nil || proxyURL.Scheme == "" || proxyURL.Host == "" {
		return "", fmt.Errorf("invalid proxy URL")
	}
	return proxyURL.Scheme + "://" + proxyURL.Host, nil
}

func logProxyEnvironment() {
	settings, noProxyName := inspectProxyEnvironment()
	if len(settings) == 0 {
		logger.Info("未检测到网络代理环境配置，SM-DP+ 请求将直连")
		return
	}
	for _, setting := range settings {
		if setting.err != nil {
			logger.Warn("网络代理环境变量格式无效", "source", setting.name, "error", setting.err)
			continue
		}
		logger.Info("已读取网络代理环境配置", "source", setting.name, "endpoint", setting.endpoint)
	}
	if noProxyName != "" {
		logger.Info("已读取网络代理绕过规则", "source", noProxyName)
	}
}
