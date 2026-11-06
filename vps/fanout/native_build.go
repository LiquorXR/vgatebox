package main

// native_build.go：入站规约组装（TLS/REALITY/分享链接/新建）。
import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// NewInboundSpec 是自建模式下新建入站的参数。
//
// 留空的字段都有合理默认：端口随机、备注按协议加端口自动生成、
// 路径随机、REALITY 的密钥与 shortId 自动生成。
type NewInboundSpec struct {
	// Internal batch provisioning may reuse one successful check of the exact
	// same destination. This field cannot be set by HTTP/JSON input.
	realityChecked bool
	BoundSlot      int // explicit stable target for a new manual entry
	Protocol       string
	Network        string
	Port           int
	Remark         string
	Path           string
	Host           string
	Security       string
	// Vision 请求给 VLESS 客户端启用 xtls-rprx-vision
	Vision bool

	// TLS：留空 CertFile 就生成自签证书
	ServerName string
	CertFile   string
	KeyFile    string

	// REALITY
	Dest        string
	ServerNames string // 逗号分隔，留空则从 Dest 推出来
	ShortID     string
	Fingerprint string

	// mKCP（VLESS + mkcp）
	KCPMTU      int
	KCPTTI      int
	KCPUplink   int
	KCPDownlink int

	// VLESS Encryption（可选）
	VLESSEncryption     bool
	VLESSEncryptionType string // x25519 | mlkem768
}

// nativeProtocols 是自建模式支持的协议，与前端下拉保持一致。
var nativeProtocols = map[string]bool{"vless": true}

// defaultEncryptedInboundSpec is the shared default for automatically created
// entry nodes: VLESS + VLESS Encryption + RAW/TCP + REALITY + Vision.
// VLESS Encryption is optional at the protocol level but enabled here so new
// entries get the stronger default. The authentication key type stays X25519
// for broad client compatibility; the ephemeral exchange remains post-quantum.
func defaultEncryptedInboundSpec(remark, dest, serverNames string) NewInboundSpec {
	return NewInboundSpec{
		Protocol: "vless", Network: "tcp", Security: "reality",
		Remark: remark, Dest: dest, ServerNames: serverNames, Fingerprint: "chrome",
		Vision:              true,
		VLESSEncryption:     true,
		VLESSEncryptionType: "x25519",
	}
}

// CreateInbound 新建一个入站，端口留空时随机分配。
func (n *Native) CreateInbound(spec NewInboundSpec, tunnels []*Tunnel) (*CreatedInbound, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	finish := n.beginChange()
	defer finish()

	var target *Tunnel
	if spec.BoundSlot < 0 {
		return nil, fmt.Errorf("出口编号无效")
	}
	if spec.BoundSlot > 0 {
		for _, t := range tunnels {
			if t.Slot == spec.BoundSlot {
				target = t
				break
			}
		}
		if target == nil {
			return nil, fmt.Errorf("选择的出口不存在")
		}
	}
	ib, err := n.prepareInbound(spec, n.store.usedPorts())
	if err != nil {
		return nil, err
	}

	ib.Manual = true
	if target != nil {
		v := target.snapshot()
		ib.BoundSlot = v.Slot
		ib.BoundTo = sanitizeTag(v.Node.HostName)
	}
	n.store.NextID++
	n.store.Inbounds = append(n.store.Inbounds, ib)

	if err := n.apply(tunnels); err != nil {
		return nil, err
	}

	return &CreatedInbound{
		ID:       ib.ID,
		Port:     ib.Port,
		Protocol: ib.Protocol,
		Remark:   ib.Remark,
		Network:  ib.netOrTCP(),
		Security: ib.securityOrNone(),
	}, nil
}

// shareLink 生成客户端可直接导入的分享链接。
func shareLink(ib *nativeInbound, c nativeClient, host string) string {
	net := ib.netOrTCP()
	sec := ib.securityOrNone()

	q := url.Values{}
	linkType := net
	if linkType == "mkcp" {
		linkType = "kcp"
	}
	q.Set("type", linkType)
	q.Set("security", sec)

	switch net {
	case "ws", "httpupgrade", "xhttp":
		q.Set("path", ib.Path)
		if ib.Host != "" {
			q.Set("host", ib.Host)
		}
	case "grpc":
		q.Set("serviceName", strings.TrimPrefix(ib.Path, "/"))
	}

	switch sec {
	case "tls":
		if ib.TLS != nil {
			if ib.TLS.ServerName != "" {
				q.Set("sni", ib.TLS.ServerName)
			}
			// 自签证书验不过 CA。Xray 26.x 移除了 allowInsecure，
			// 改用证书指纹让客户端固定信任这一张。pcs 是 Xray/v2rayN
			// 共用的标准字段；旧的 pinSHA256 只保留给兼容解析。
			if ib.TLS.SelfSigned && ib.TLS.CertSha256 != "" {
				q.Set("pcs", ib.TLS.CertSha256)
			}
		}
	case "reality":
		if ib.Reality != nil {
			if len(ib.Reality.ServerNames) > 0 {
				q.Set("sni", ib.Reality.ServerNames[0])
			}
			// pbk 是分享链接的通用写法，各家客户端都认；
			// 注意 Xray 26.x 自己的配置文件里这个字段叫 password 而不是 publicKey
			q.Set("pbk", ib.Reality.PublicKey)
			if len(ib.Reality.ShortIDs) > 0 {
				q.Set("sid", ib.Reality.ShortIDs[0])
			}
			if ib.Reality.Fingerprint != "" {
				q.Set("fp", ib.Reality.Fingerprint)
			}
		}
	}

	if c.Flow != "" && ib.Protocol == "vless" {
		q.Set("flow", c.Flow)
	}

	frag := url.PathEscape(ib.Remark)

	// VLESS Encryption 是协议层参数，所有传输都可以叠加。
	if ib.VLESSEncryption != nil && ib.VLESSEncryption.Enabled && ib.VLESSEncryption.ClientEncryption != "" {
		q.Set("encryption", ib.VLESSEncryption.ClientEncryption)
	} else {
		q.Set("encryption", "none")
	}
	return fmt.Sprintf("vless://%s@%s:%d?%s#%s", c.ID, host, ib.Port, q.Encode(), frag)
}

// buildTLS 组装 TLS 配置。没给证书路径就生成一张自签的，落在 dir/certs 下。
func buildTLS(dir string, spec NewInboundSpec) (*tlsConfig, error) {
	name := strings.TrimSpace(spec.ServerName)
	if name == "" {
		name = "localhost"
	}
	conf := &tlsConfig{ServerName: name}

	cert, key := strings.TrimSpace(spec.CertFile), strings.TrimSpace(spec.KeyFile)
	// 只填一个多半是漏填，静默退回自签会让用户以为用上了自己的证书
	if (cert == "") != (key == "") {
		return nil, fmt.Errorf("证书和私钥要成对填写，或者都留空用自签证书")
	}
	if cert != "" && key != "" {
		if _, err := os.Stat(cert); err != nil {
			return nil, fmt.Errorf("证书文件不可读: %w", err)
		}
		if _, err := os.Stat(key); err != nil {
			return nil, fmt.Errorf("私钥文件不可读: %w", err)
		}
		conf.CertFile, conf.KeyFile = cert, key
		return conf, nil
	}

	// 自签证书验不过 CA，靠链接里的证书指纹让客户端固定信任
	c, k, err := selfSignedCert(dir, name)
	if err != nil {
		return nil, err
	}
	conf.CertFile, conf.KeyFile, conf.SelfSigned = c, k, true
	// 指纹是自签证书唯一能让客户端验过的凭据，算不出来就没法生成可用链接
	fp, err := certFingerprint(c)
	if err != nil {
		os.Remove(c)
		os.Remove(k)
		return nil, err
	}
	conf.CertSha256 = fp
	return conf, nil
}

// buildReality 组装 REALITY 配置，密钥和 shortId 都自动生成。
// xrayBin 用来跑 `xray x25519` 生成密钥对。
func buildReality(xrayBin string, spec NewInboundSpec) (*realityConfig, error) {
	dest := strings.TrimSpace(spec.Dest)
	if dest == "" {
		// REALITY 要跟 dest 完成一次真实 TLS1.3 握手，dest 不稳会让所有连接
		// 静默回落。默认使用 Tesla 站点。
		dest = "www.tesla.com:443"
	}
	if !strings.Contains(dest, ":") {
		dest += ":443"
	}

	var names []string
	for _, s := range strings.Split(spec.ServerNames, ",") {
		if s = strings.TrimSpace(s); s != "" {
			names = append(names, s)
		}
	}
	if len(names) == 0 {
		// 默认用 dest 的主机名：REALITY 要求 SNI 与被借用的站点一致
		names = []string{strings.SplitN(dest, ":", 2)[0]}
	}

	priv, pub, err := realityKeys(xrayBin)
	if err != nil {
		return nil, err
	}
	if !spec.realityChecked {
		if err := checkRealityDest(dest, names[0]); err != nil {
			return nil, fmt.Errorf("REALITY 目标站点不可用，换一个 dest: %w", err)
		}
	}

	short := strings.TrimSpace(spec.ShortID)
	if short == "" {
		short = randomShortID()
	}
	fp := strings.TrimSpace(spec.Fingerprint)
	if fp == "" {
		fp = "chrome"
	}

	return &realityConfig{
		Dest:        dest,
		ServerNames: names,
		PrivateKey:  priv,
		PublicKey:   pub,
		ShortIDs:    []string{short},
		Fingerprint: fp,
	}, nil
}

func (n *Native) prepareInbound(spec NewInboundSpec, used map[int]bool) (*nativeInbound, error) {
	ns, err := normalizeInboundSpec(spec, used)
	if err != nil {
		return nil, err
	}
	proto, network, security, port := ns.Protocol, ns.Network, ns.Security, ns.Port

	ib := &nativeInbound{
		NameMode: "custom",
		ID:       n.store.NextID,
		Port:     port,
		Protocol: proto,
		Network:  network,
		Path:     ns.Path,
		Host:     ns.Host,
		Security: security,
		Remark:   ns.Remark,
		Enable:   true,
		KCP:      ns.KCP,
	}

	switch security {
	case "tls":
		conf, err := buildTLS(n.dir, spec)
		if err != nil {
			return nil, err
		}
		ib.TLS = conf
	case "reality":
		conf, err := buildReality(n.proc.bin, spec)
		if err != nil {
			return nil, err
		}
		ib.Reality = conf
	}

	if ns.VLESSEncryption != nil {
		enc, err := newVLESSEncryption(n.proc.bin, ns.VLESSEncryption.KeyType)
		if err != nil {
			return nil, err
		}
		ib.VLESSEncryption = enc
	}

	client := nativeClient{
		ID:       newUUID(),
		Password: randomHex(8),
		Flow:     ns.Flow,
		Enable:   true,
	}
	client.Email = fmt.Sprintf("%s-%d", proto, port)
	ib.Clients = []nativeClient{client}

	return ib, nil
}
