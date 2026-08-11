package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/upstreamproxy"
)

// UpstreamProxy 前置代理实例（用于代理 VoWiFi 的 ePDG 连接）。
// 通过 Socks5 UDP Associate 将 IKE/ESP 流量转发到 ePDG。
// 语义移植自 vohive-open internal/db/upstream_proxy.go（gorm → raw SQL）。
type UpstreamProxy struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Addr      string    `json:"addr"` // Socks5 服务器地址 (host:port)
	Username  string    `json:"username"`
	Password  string    `json:"password,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UpstreamProxyCountryRule 将 SIM home country 路由到指定前置代理。
type UpstreamProxyCountryRule struct {
	CountryCode     string    `json:"country_code"`
	UpstreamProxyID string    `json:"upstream_proxy_id"`
	Enabled         bool      `json:"enabled"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ListUpstreamProxies 列出所有前置代理实例。
func (s *SQLiteStore) ListUpstreamProxies() ([]UpstreamProxy, error) {
	rows, err := s.db.Query(`
		SELECT id, name, addr, username, password, enabled, created_at, updated_at
		FROM upstream_proxies ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list upstream proxies: %w", err)
	}
	defer rows.Close()
	out := make([]UpstreamProxy, 0)
	for rows.Next() {
		p, err := scanUpstreamProxy(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upstream proxies: %w", err)
	}
	return out, nil
}

// scanUpstreamProxy 扫描一行 upstream_proxies（时间列为 RFC3339Nano 文本）。
func scanUpstreamProxy(scan func(...any) error) (UpstreamProxy, error) {
	var p UpstreamProxy
	var enabled int
	var createdAt, updatedAt string
	if err := scan(&p.ID, &p.Name, &p.Addr, &p.Username, &p.Password,
		&enabled, &createdAt, &updatedAt); err != nil {
		return p, fmt.Errorf("scan upstream proxy: %w", err)
	}
	p.Enabled = enabled != 0
	var err error
	p.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return p, fmt.Errorf("parse upstream proxy created_at: %w", err)
	}
	p.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return p, fmt.Errorf("parse upstream proxy updated_at: %w", err)
	}
	return p, nil
}

// GetUpstreamProxyByID 根据 ID 获取前置代理；缺失返回 (nil, nil)。
func (s *SQLiteStore) GetUpstreamProxyByID(id string) (*UpstreamProxy, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("empty id")
	}
	row := s.db.QueryRow(`
		SELECT id, name, addr, username, password, enabled, created_at, updated_at
		FROM upstream_proxies WHERE id = ?
	`, id)
	p, err := scanUpstreamProxy(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertUpstreamProxy 创建或更新前置代理。
func (s *SQLiteStore) UpsertUpstreamProxy(p UpstreamProxy) error {
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("empty id")
	}
	if strings.TrimSpace(p.Addr) == "" {
		return errors.New("empty addr")
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	_, err := s.db.Exec(`
		INSERT INTO upstream_proxies(id, name, addr, username, password, enabled, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			addr = excluded.addr,
			username = excluded.username,
			password = excluded.password,
			enabled = excluded.enabled,
			updated_at = excluded.updated_at
	`, p.ID, p.Name, p.Addr, p.Username, p.Password, boolInt(p.Enabled),
		p.CreatedAt.Format(time.RFC3339Nano), p.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert upstream proxy: %w", err)
	}
	return nil
}

// DeleteUpstreamProxy 删除前置代理（同时清理关联的国家规则）。
func (s *SQLiteStore) DeleteUpstreamProxy(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("empty id")
	}
	if _, err := s.db.Exec(`DELETE FROM upstream_proxy_country_rules WHERE upstream_proxy_id = ?`, id); err != nil {
		return fmt.Errorf("delete upstream proxy country rules: %w", err)
	}
	if _, err := s.db.Exec(`DELETE FROM upstream_proxies WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete upstream proxy: %w", err)
	}
	return nil
}

// ListUpstreamProxyCountryRules 列出所有国家规则。
func (s *SQLiteStore) ListUpstreamProxyCountryRules() ([]UpstreamProxyCountryRule, error) {
	rows, err := s.db.Query(`
		SELECT country_code, upstream_proxy_id, enabled, updated_at
		FROM upstream_proxy_country_rules ORDER BY country_code ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list upstream proxy country rules: %w", err)
	}
	defer rows.Close()
	out := make([]UpstreamProxyCountryRule, 0)
	for rows.Next() {
		var r UpstreamProxyCountryRule
		var enabled int
		var updatedAt string
		if err := rows.Scan(&r.CountryCode, &r.UpstreamProxyID, &enabled, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan upstream proxy country rule: %w", err)
		}
		r.Enabled = enabled != 0
		var err error
		r.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse upstream proxy country rule updated_at: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate upstream proxy country rules: %w", err)
	}
	return out, nil
}

// UpsertUpstreamProxyCountryRule 创建或更新国家规则。
func (s *SQLiteStore) UpsertUpstreamProxyCountryRule(rule UpstreamProxyCountryRule) error {
	rule.CountryCode = upstreamproxy.NormalizeCountryCode(rule.CountryCode)
	rule.UpstreamProxyID = strings.TrimSpace(rule.UpstreamProxyID)
	if rule.CountryCode == "" {
		return errors.New("empty country_code")
	}
	if rule.UpstreamProxyID == "" {
		return errors.New("empty upstream_proxy_id")
	}
	rule.UpdatedAt = time.Now().UTC()
	_, err := s.db.Exec(`
		INSERT INTO upstream_proxy_country_rules(country_code, upstream_proxy_id, enabled, updated_at)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(country_code) DO UPDATE SET
			upstream_proxy_id = excluded.upstream_proxy_id,
			enabled = excluded.enabled,
			updated_at = excluded.updated_at
	`, rule.CountryCode, rule.UpstreamProxyID, boolInt(rule.Enabled),
		rule.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert upstream proxy country rule: %w", err)
	}
	return nil
}

// DeleteUpstreamProxyCountryRule 删除国家规则。
func (s *SQLiteStore) DeleteUpstreamProxyCountryRule(countryCode string) error {
	countryCode = upstreamproxy.NormalizeCountryCode(countryCode)
	if countryCode == "" {
		return errors.New("empty country_code")
	}
	if _, err := s.db.Exec(`DELETE FROM upstream_proxy_country_rules WHERE country_code = ?`, countryCode); err != nil {
		return fmt.Errorf("delete upstream proxy country rule: %w", err)
	}
	return nil
}

// GetCountryUpstreamProxy 解析国家规则为启用的代理；未命中或未启用返回 (nil, nil)。
func (s *SQLiteStore) GetCountryUpstreamProxy(countryCode string) (*UpstreamProxy, error) {
	countryCode = upstreamproxy.NormalizeCountryCode(countryCode)
	if countryCode == "" {
		return nil, nil
	}
	var rule UpstreamProxyCountryRule
	var enabled int
	var updatedAt string
	err := s.db.QueryRow(`
		SELECT country_code, upstream_proxy_id, enabled, updated_at
		FROM upstream_proxy_country_rules WHERE country_code = ?
	`, countryCode).Scan(&rule.CountryCode, &rule.UpstreamProxyID, &enabled, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get country upstream proxy: %w", err)
	}
	rule.Enabled = enabled != 0
	rule.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse country upstream proxy rule updated_at: %w", err)
	}
	if !rule.Enabled || strings.TrimSpace(rule.UpstreamProxyID) == "" {
		return nil, nil
	}
	proxy, err := s.GetUpstreamProxyByID(rule.UpstreamProxyID)
	if err != nil || proxy == nil || !proxy.Enabled {
		return nil, err
	}
	return proxy, nil
}

// GetHomeMCCUpstreamProxy 由 SIM home MCC 解析国家前置代理：
// MCC → 国家码（upstreamproxy 国家表）→ 国家规则 → 代理。
// 返回 (proxy, countryCode, error)；未命中时 proxy 为 nil。
func (s *SQLiteStore) GetHomeMCCUpstreamProxy(homeMCC string) (*UpstreamProxy, string, error) {
	countryCode, ok := upstreamproxy.CountryCodeFromHomeMCC(homeMCC)
	if !ok {
		return nil, "", nil
	}
	proxy, err := s.GetCountryUpstreamProxy(countryCode)
	return proxy, countryCode, err
}
