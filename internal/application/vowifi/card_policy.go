package vowifi

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/storage"
)

var errCardPolicyEmptyICCID = errors.New("card policy iccid is empty")

// 卡策略持久化（本仓库新增，非上游代码）。按 handoff 方案用
// database.Namespace 的 JSON 文档保存卡片级 VoWiFi 开关：
// 有策略行且 VoWiFiEnabled=false 时阻断期望态恢复；无策略行默认允许
// （保持未接线前的行为，不自动建档）。
const cardPoliciesNamespace = "vowifi_card_policies"

// CardPolicy 是跟随卡(ICCID)走的 VoWiFi 策略开关。
type CardPolicy struct {
	ICCID         string    `json:"iccid"`
	VoWiFiEnabled bool      `json:"vowifi_enabled"`
	Source        string    `json:"source"` // auto | user
	UpdatedAt     time.Time `json:"updated_at"`
}

// CardPolicyStore 包装 app_settings 命名空间下的 JSON map。
type CardPolicyStore struct {
	store storage.ValueStore
}

// NewCardPolicyStore 由根存储构造卡策略视图；database 为 nil 时返回 nil。
func NewCardPolicyStore(database *storage.SQLiteStore) *CardPolicyStore {
	if database == nil {
		return nil
	}
	return &CardPolicyStore{store: database.Namespace(cardPoliciesNamespace)}
}

type cardPoliciesDocument struct {
	Policies map[string]CardPolicy `json:"policies"`
}

// canonicalICCID 规整 ICCID：trim、去引号、去尾部 BCD 填充位 F/f
// （上游 db.CanonicalICCID 语义）。
func canonicalICCID(iccid string) string {
	v := strings.TrimSpace(iccid)
	v = strings.Trim(v, "\"")
	return strings.TrimRight(v, "Ff")
}

func (p *CardPolicyStore) readDocument() (cardPoliciesDocument, error) {
	doc := cardPoliciesDocument{Policies: map[string]CardPolicy{}}
	if p == nil || p.store == nil {
		return doc, nil
	}
	if err := p.store.Read(&doc); err != nil {
		return doc, err
	}
	if doc.Policies == nil {
		doc.Policies = map[string]CardPolicy{}
	}
	return doc, nil
}

// Get 读取卡片策略；缺失返回 (CardPolicy{}, false)（调用方视为允许）。
func (p *CardPolicyStore) Get(iccid string) (CardPolicy, bool) {
	iccid = canonicalICCID(iccid)
	if iccid == "" {
		return CardPolicy{}, false
	}
	doc, err := p.readDocument()
	if err != nil {
		return CardPolicy{}, false
	}
	pol, ok := doc.Policies[iccid]
	return pol, ok
}

// List 列出全部策略（按 ICCID 排序）。
func (p *CardPolicyStore) List() []CardPolicy {
	doc, err := p.readDocument()
	if err != nil {
		return nil
	}
	out := make([]CardPolicy, 0, len(doc.Policies))
	for _, pol := range doc.Policies {
		out = append(out, pol)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ICCID < out[j].ICCID })
	return out
}

// Upsert 写入/更新卡片策略。ICCID 为空时报错。
func (p *CardPolicyStore) Upsert(pol CardPolicy) error {
	if p == nil || p.store == nil {
		return nil
	}
	pol.ICCID = canonicalICCID(pol.ICCID)
	if pol.ICCID == "" {
		return errCardPolicyEmptyICCID
	}
	if strings.TrimSpace(pol.Source) == "" {
		pol.Source = "user"
	}
	pol.UpdatedAt = time.Now().UTC()
	doc, err := p.readDocument()
	if err != nil {
		return err
	}
	doc.Policies[pol.ICCID] = pol
	return p.store.Write(doc)
}

// AllowsVoWiFi 是期望态门控语义：无策略行（含 store 缺失）视为允许。
func (p *CardPolicyStore) AllowsVoWiFi(iccid string) bool {
	if p == nil {
		return true
	}
	pol, ok := p.Get(iccid)
	if !ok {
		return true
	}
	return pol.VoWiFiEnabled
}

// CardPolicyList 列出全部卡片策略（管理 API 使用）。
func (s *Service) CardPolicyList() []CardPolicy {
	if s == nil || s.cardPolicies == nil {
		return nil
	}
	return s.cardPolicies.List()
}

// CardPolicyGet 读取单卡策略；缺失返回 (CardPolicy{}, false)。
func (s *Service) CardPolicyGet(iccid string) (CardPolicy, bool) {
	if s == nil || s.cardPolicies == nil {
		return CardPolicy{}, false
	}
	return s.cardPolicies.Get(iccid)
}

// CardPolicySet 写入单卡 VoWiFi 开关；成功返回 (true, nil)。
// 未注入 store 时返回 (false, nil)。
func (s *Service) CardPolicySet(iccid string, enabled bool) (bool, error) {
	if s == nil || s.cardPolicies == nil {
		return false, nil
	}
	pol, _ := s.cardPolicies.Get(iccid)
	pol.ICCID = iccid
	pol.VoWiFiEnabled = enabled
	if err := s.cardPolicies.Upsert(pol); err != nil {
		return false, err
	}
	return true, nil
}
