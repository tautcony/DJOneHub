package httpapi

import (
	"fmt"
	nethttp "net/http"
	"strings"

	"github.com/iniwex5/vohive/internal/application/vowifi"
	derrors "github.com/iniwex5/vohive/internal/domain/errors"
)

// VoWiFi 管理 API：国家前置代理与卡策略的持久化读写。
// 读写均走 query 参数 + 方法分派（仓库无 path-param 先例）。

type vowifiProxyRequest struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Addr     string `json:"addr"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	Enabled  bool   `json:"enabled"`
}

type vowifiCountryRuleRequest struct {
	CountryCode     string `json:"country_code"`
	UpstreamProxyID string `json:"upstream_proxy_id"`
	Enabled         bool   `json:"enabled"`
}

type vowifiCardPolicyRequest struct {
	VoWiFiEnabled bool `json:"vowifi_enabled"`
}

func (s *Server) vowifiProxies(w nethttp.ResponseWriter, r *nethttp.Request) {
	if s.config.VoWiFi == nil {
		writeError(w, fmt.Errorf("vowifi service is unavailable"))
		return
	}
	switch r.Method {
	case nethttp.MethodGet:
		if !s.protected(w, r) {
			return
		}
		proxies, err := s.config.VoWiFi.ProxyList()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, proxies)
	case nethttp.MethodPost:
		if !s.commandOnly(w, r) {
			return
		}
		var request vowifiProxyRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, derrors.New(derrors.InvalidRequest, "invalid JSON request", false, nil))
			return
		}
		if strings.TrimSpace(request.Addr) == "" {
			writeError(w, derrors.New(derrors.InvalidRequest, "addr is required", false, nil))
			return
		}
		if err := s.config.VoWiFi.ProxyUpsert(vowifi.UpstreamProxy{
			ID:       request.ID,
			Name:     request.Name,
			Addr:     request.Addr,
			Username: request.Username,
			Password: request.Password,
			Enabled:  request.Enabled,
		}); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, map[string]any{"ok": true})
	case nethttp.MethodDelete:
		if !s.requireMethod(w, r, nethttp.MethodDelete) || !s.protected(w, r) {
			return
		}
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			writeError(w, derrors.New(derrors.InvalidRequest, "id is required", false, nil))
			return
		}
		if err := s.config.VoWiFi.ProxyDelete(id); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, map[string]any{"ok": true})
	default:
		s.requireMethod(w, r, nethttp.MethodGet)
	}
}

func (s *Server) vowifiProxyCountryRules(w nethttp.ResponseWriter, r *nethttp.Request) {
	if s.config.VoWiFi == nil {
		writeError(w, fmt.Errorf("vowifi service is unavailable"))
		return
	}
	switch r.Method {
	case nethttp.MethodGet:
		if !s.protected(w, r) {
			return
		}
		rules, err := s.config.VoWiFi.CountryRuleList()
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, rules)
	case nethttp.MethodPost:
		if !s.commandOnly(w, r) {
			return
		}
		var request vowifiCountryRuleRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, derrors.New(derrors.InvalidRequest, "invalid JSON request", false, nil))
			return
		}
		if err := s.config.VoWiFi.CountryRuleUpsert(vowifi.UpstreamProxyCountryRule{
			CountryCode:     request.CountryCode,
			UpstreamProxyID: request.UpstreamProxyID,
			Enabled:         request.Enabled,
		}); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, map[string]any{"ok": true})
	case nethttp.MethodDelete:
		if !s.requireMethod(w, r, nethttp.MethodDelete) || !s.protected(w, r) {
			return
		}
		countryCode := strings.TrimSpace(r.URL.Query().Get("country_code"))
		if countryCode == "" {
			writeError(w, derrors.New(derrors.InvalidRequest, "country_code is required", false, nil))
			return
		}
		if err := s.config.VoWiFi.CountryRuleDelete(countryCode); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, nethttp.StatusOK, map[string]any{"ok": true})
	default:
		s.requireMethod(w, r, nethttp.MethodGet)
	}
}

func (s *Server) vowifiCountryTable(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !s.requireMethod(w, r, nethttp.MethodGet) || !s.protected(w, r) {
		return
	}
	if s.config.VoWiFi == nil {
		writeError(w, fmt.Errorf("vowifi service is unavailable"))
		return
	}
	writeJSON(w, nethttp.StatusOK, s.config.VoWiFi.CountryTableStatus())
}

func (s *Server) vowifiCardPolicies(w nethttp.ResponseWriter, r *nethttp.Request) {
	if s.config.VoWiFi == nil {
		writeError(w, fmt.Errorf("vowifi service is unavailable"))
		return
	}
	switch r.Method {
	case nethttp.MethodGet:
		if !s.protected(w, r) {
			return
		}
		writeJSON(w, nethttp.StatusOK, s.config.VoWiFi.CardPolicyList())
	case nethttp.MethodPut:
		if !s.requireMethod(w, r, nethttp.MethodPut) || !s.protected(w, r) {
			return
		}
		iccid := strings.TrimSpace(r.URL.Query().Get("iccid"))
		if iccid == "" {
			writeError(w, derrors.New(derrors.InvalidRequest, "iccid is required", false, nil))
			return
		}
		var request vowifiCardPolicyRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, derrors.New(derrors.InvalidRequest, "invalid JSON request", false, nil))
			return
		}
		persisted, err := s.config.VoWiFi.CardPolicySet(iccid, request.VoWiFiEnabled)
		if err != nil {
			writeError(w, err)
			return
		}
		if !persisted {
			writeError(w, derrors.New(derrors.InvalidRequest, "card policy store is unavailable", false, nil))
			return
		}
		writeJSON(w, nethttp.StatusOK, map[string]any{"ok": true})
	default:
		s.requireMethod(w, r, nethttp.MethodGet)
	}
}
