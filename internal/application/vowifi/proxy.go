package vowifi

import (
	"errors"

	"github.com/iniwex5/vohive/internal/storage"
	"github.com/iniwex5/vohive/internal/upstreamproxy"
)

// 国家前置代理管理（本仓库新增接线，非上游代码）。CRUD 走 storage 层，
// HTTP 层经 httpapi.Config 注入使用；命中规则在下次 StartRuntime 时生效。

// UpstreamProxy 是管理 API 使用的代理视图（别名 storage 类型，避免
// HTTP 层依赖 storage 包细节）。
type UpstreamProxy = storage.UpstreamProxy

// UpstreamProxyCountryRule 是国家规则视图。
type UpstreamProxyCountryRule = storage.UpstreamProxyCountryRule

// CountryTableStatus 是国家表（MCC→国家码）当前状态。
type CountryTableStatus struct {
	Ready     bool   `json:"ready"`
	CachePath string `json:"cache_path,omitempty"`
	SourceURL string `json:"source_url,omitempty"`
	Source    string `json:"source,omitempty"`
	RowCount  int    `json:"row_count,omitempty"`
	Countries int    `json:"countries,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (s *Service) proxyStore() *storage.SQLiteStore {
	if s == nil || s.adapter == nil {
		return nil
	}
	return s.adapter.store
}

// ProxyList 列出全部前置代理。
func (s *Service) ProxyList() ([]UpstreamProxy, error) {
	store := s.proxyStore()
	if store == nil {
		return nil, errors.New("持久化层未注入（voWiFi.SetStore）")
	}
	return store.ListUpstreamProxies()
}

// ProxyUpsert 创建或更新前置代理。
func (s *Service) ProxyUpsert(p UpstreamProxy) error {
	store := s.proxyStore()
	if store == nil {
		return errors.New("持久化层未注入（voWiFi.SetStore）")
	}
	return store.UpsertUpstreamProxy(p)
}

// ProxyDelete 删除前置代理（连带清理国家规则）。
func (s *Service) ProxyDelete(id string) error {
	store := s.proxyStore()
	if store == nil {
		return errors.New("持久化层未注入（voWiFi.SetStore）")
	}
	return store.DeleteUpstreamProxy(id)
}

// CountryRuleList 列出全部国家规则。
func (s *Service) CountryRuleList() ([]UpstreamProxyCountryRule, error) {
	store := s.proxyStore()
	if store == nil {
		return nil, errors.New("持久化层未注入（voWiFi.SetStore）")
	}
	return store.ListUpstreamProxyCountryRules()
}

// CountryRuleUpsert 创建或更新国家规则。
func (s *Service) CountryRuleUpsert(rule UpstreamProxyCountryRule) error {
	store := s.proxyStore()
	if store == nil {
		return errors.New("持久化层未注入（voWiFi.SetStore）")
	}
	return store.UpsertUpstreamProxyCountryRule(rule)
}

// CountryRuleDelete 删除国家规则。
func (s *Service) CountryRuleDelete(countryCode string) error {
	store := s.proxyStore()
	if store == nil {
		return errors.New("持久化层未注入（voWiFi.SetStore）")
	}
	return store.DeleteUpstreamProxyCountryRule(countryCode)
}

// CountryTableStatus 返回国家表状态（InitCountryTable 的结果快照）。
// 未初始化时以表格 ready 标志表达。
func (s *Service) CountryTableStatus() CountryTableStatus {
	status := CountryTableStatus{Ready: upstreamproxy.CountryTableReady()}
	if !status.Ready {
		status.Error = "国家表未就绪：MCC→国家码解析不可用，国家代理规则不会命中"
	}
	return status
}
