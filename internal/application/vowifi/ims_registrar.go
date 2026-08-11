package vowifi

import (
	"os"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost"
)

// IMS 注册器构造（本仓库新增接线，非上游代码）。
//
// 引擎的 runtimehost.WireIMSRegistrar 在 RegisterIMS 时才会拿到会话数据
// （IMSRegistrationConfig，含隧道结果与 SIM AKA provider），因此这里只需
// 提供网络面默认值：
//   - ServerAddr/Resolver 留空：引擎按 registrar URI（profile 域
//     ims.mnc<MNC>.mcc<MCC>.3gppnetwork.org）经隧道 DNS 构建的 NetSIPResolver
//     做 SRV 解析定位 P-CSCF（third_party/vowifi-go/runtimehost/imsregistrar.go
//     resolverForConfig / defaultSIPFlow）；
//   - ContactHost 留空：回退 profile.LocalIP（隧道 LocalInnerIP）。
//
// 环境变量覆盖（真机/测试 IMS 环境验证用）：
//   - DJONEHUB_VOWIFI_IMS_REGISTRAR：指定 registrar URI（如 sip:ims.example.com）
//   - DJONEHUB_VOWIFI_IMS_SERVER：直接指定 P-CSCF 地址（host:port），跳过解析
func buildIMSRegistrar() runtimehost.WireIMSRegistrar {
	reg := runtimehost.WireIMSRegistrar{
		Network:   "udp",
		Timeout:   10 * time.Second,
		Expires:   3600,
		UserAgent: "vowifi-go/djonehub",
	}
	if uri := strings.TrimSpace(os.Getenv("DJONEHUB_VOWIFI_IMS_REGISTRAR")); uri != "" {
		reg.RegistrarURI = uri
	}
	if addr := strings.TrimSpace(os.Getenv("DJONEHUB_VOWIFI_IMS_SERVER")); addr != "" {
		reg.ServerAddr = addr
	}
	return reg
}
