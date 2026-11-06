package main

// native_inbounds.go：入站与客户端的增删改查与绑定。
import (
	"fmt"
	"strings"
)

func (n *Native) Inbounds(live map[string]bool) ([]Inbound, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.unavailable {
		return nil, fmt.Errorf("配置运行恢复未完成，内置 Xray 保持阻断")
	}

	list := n.store.sorted()
	out := make([]Inbound, 0, len(list))
	for _, ib := range list {
		if ib.SocksSlot > 0 || ib.ThirdParty != nil {
			continue
		}
		boundUp := live[ib.BoundTo]
		if ib.BoundSlot > 0 {
			boundUp = live[slotOutboundTag(ib.BoundSlot)]
		}
		out = append(out, Inbound{
			ID: ib.ID, Port: ib.Port, Protocol: ib.Protocol,
			Remark: ib.Remark, Enable: ib.Enable, Tag: ib.tag(),
			HostDirect: ib.HostDirect, ManagedSlot: ib.ManagedSlot, Manual: ib.Manual, BoundTo: ib.BoundTo, BoundUp: boundUp, BoundSlot: ib.BoundSlot, NameMode: inboundNameMode(ib.NameMode, ib.Remark),
		})
	}
	return out, nil
}

func (n *Native) InboundDetail(id int, publicHost string) (*InboundDetail, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.unavailable {
		return nil, fmt.Errorf("配置恢复未完成，订阅服务已阻断")
	}

	ib := n.store.byID(id)
	if ib == nil {
		return nil, fmt.Errorf("入站 %d 不存在", id)
	}
	detail := &InboundDetail{
		Inbound: Inbound{
			ID: ib.ID, Port: ib.Port, Protocol: ib.Protocol,
			Remark: ib.Remark, Enable: ib.Enable, Tag: ib.tag(),
			HostDirect: ib.HostDirect, ManagedSlot: ib.ManagedSlot, Manual: ib.Manual, BoundTo: ib.BoundTo, BoundSlot: ib.BoundSlot, NameMode: inboundNameMode(ib.NameMode, ib.Remark),
		},
		Listen:  "0.0.0.0",
		Network: ib.netOrTCP(),
		TLS:     ib.securityOrNone(),
	}
	if ib.VLESSEncryption != nil && ib.VLESSEncryption.Enabled {
		detail.VLESSEncryption = true
		detail.VLESSEncryptionType = ib.VLESSEncryption.KeyType
	}
	for _, c := range ib.Clients {
		detail.Clients = append(detail.Clients, ClientInfo{Email: c.Email, ID: c.ID, Enable: c.Enable})
		detail.Links = append(detail.Links, shareLink(ib, c, publicHost))
	}
	return detail, nil
}

func (n *Native) InboundLinks(ids []int, publicHost string) ([]string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.unavailable {
		return nil, fmt.Errorf("配置恢复未完成，订阅服务已阻断")
	}

	var out []string
	for _, id := range ids {
		ib := n.store.byID(id)
		if ib == nil || ib.Protocol != "vless" || ib.SocksSlot > 0 {
			continue
		}
		for _, c := range ib.Clients {
			if !c.Enable {
				continue
			}
			if link := shareLink(ib, c, publicHost); link != "" {
				out = append(out, link)
			}
		}
	}
	return out, nil
}

func (n *Native) Bind(inboundTag string, hostname string, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	finish := n.beginChange()
	defer finish()

	var target *Tunnel
	if hostname != "" {
		for _, t := range tunnels {
			if t.Node.HostName == hostname {
				target = t
				break
			}
		}
		if target == nil {
			return fmt.Errorf("节点 %s 没有运行中的隧道", hostname)
		}
		if target.Status != "up" {
			return fmt.Errorf("节点 %s 的隧道还没连通（当前 %s）", hostname, target.Status)
		}
	}

	var found *nativeInbound
	for _, ib := range n.store.Inbounds {
		if ib.tag() == inboundTag {
			found = ib
			break
		}
	}
	if found == nil {
		return fmt.Errorf("入站 %s 不存在", inboundTag)
	}

	if found.HostDirect || found.SocksSlot > 0 || found.ThirdParty != nil || found.ManagedSlot {
		return fmt.Errorf("此入口由本机直出或 SOCKS 访问设置管理")
	}
	if target == nil {
		found.BoundTo = ""
		found.BoundSlot = 0
	} else {
		found.BoundTo = sanitizeTag(target.Node.HostName)
		found.BoundSlot = target.Slot
	}
	return n.apply(tunnels)
}

func (n *Native) Rebind(oldHost string, target *Tunnel, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	finish := n.beginChange()
	defer finish()

	oldTag := sanitizeTag(oldHost)
	newTag := sanitizeTag(target.Node.HostName)
	newLabel := exitLabel(target)
	for _, ib := range n.store.Inbounds {
		if ib.BoundSlot > 0 {
			if ib.BoundSlot != target.Slot {
				continue
			}
		} else if oldTag == "" || ib.BoundTo != oldTag {
			continue
		}
		ib.BoundTo = newTag
		ib.BoundSlot = target.Slot
		// 备注里带着旧出口的地区和 IP 尾段，换了节点要跟着改
		if ib.NameMode != "custom" {
			ib.Remark = renameExitLabel(ib.Remark, newLabel)
		}
	}
	return n.apply(tunnels)
}

func (n *Native) ResyncOutbound(t *Tunnel, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.apply(tunnels)
}

// CloneToTunnels 以某个入站为模板，为每条指定隧道复制一个入站并绑好出口。
//
// 客户端凭据整套沿用模板：同一个 UUID 能走所有出口，用户只改端口。
func (n *Native) CloneToTunnels(templateID int, hosts []string, tunnels []*Tunnel) ([]int, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	finish := n.beginChange()
	defer finish()

	tpl := n.store.byID(templateID)
	if tpl == nil {
		return nil, fmt.Errorf("模板入站 %d 不存在", templateID)
	}
	if tpl.ThirdParty != nil {
		return nil, fmt.Errorf("第三方代理请在代理资源中管理")
	}

	byHost := map[string]*Tunnel{}
	for _, t := range tunnels {
		byHost[t.Node.HostName] = t
	}

	used := n.store.usedPorts()
	// 别名撞了客户端会丢节点，先把已有备注收进来避让
	takenRemarks := map[string]bool{}
	for _, ib := range n.store.Inbounds {
		takenRemarks[strings.TrimSpace(ib.Remark)] = true
	}
	created := []int{}
	for _, host := range hosts {
		t := byHost[host]
		if t == nil || t.Status != "up" {
			continue
		}
		port, err := freeRandomPortFor(tpl.netOrTCP(), used)
		if err != nil {
			return nil, err
		}
		used[port] = true

		remark := uniqueRemark(exitLabel(t), takenRemarks)
		takenRemarks[remark] = true

		clone := &nativeInbound{
			ID:       n.store.NextID,
			Port:     port,
			Protocol: tpl.Protocol,
			Network:  tpl.Network,
			Path:     tpl.Path,
			Host:     tpl.Host,
			// 安全层必须跟着复制：漏掉的话从 REALITY/TLS 模板复制出来的
			// 入站会变成明文，而分享链接照样标着模板的协议，很难发现
			Security:        tpl.Security,
			TLS:             tpl.TLS,
			Reality:         tpl.Reality,
			KCP:             tpl.KCP,
			VLESSEncryption: tpl.VLESSEncryption,
			Remark:          remark,
			Enable:          true,
			Manual:          true,
			Clients:         append([]nativeClient(nil), tpl.Clients...),
			BoundTo:         sanitizeTag(t.Node.HostName),
			BoundSlot:       t.Slot, NameMode: "auto",
		}
		n.store.NextID++
		n.store.Inbounds = append(n.store.Inbounds, clone)
		created = append(created, port)
	}

	if len(created) == 0 {
		return nil, fmt.Errorf("没有可用的隧道")
	}
	if err := n.apply(tunnels); err != nil {
		return nil, err
	}
	return created, nil
}

func (n *Native) DeleteInbounds(ids []int, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	finish := n.beginChange()
	defer finish()

	drop := map[int]bool{}
	for _, id := range ids {
		drop[id] = true
	}
	for _, ib := range n.store.Inbounds {
		if drop[ib.ID] && ib.ManagedSlot {
			for _, t := range tunnels {
				if t.Slot == ib.BoundSlot {
					return fmt.Errorf("自动入口由出口数量维护，请停用入口或在出口管理删除对应出口")
				}
			}
		}
	}
	kept := make([]*nativeInbound, 0, len(n.store.Inbounds))
	for _, ib := range n.store.Inbounds {
		if !drop[ib.ID] {
			kept = append(kept, ib)
		}
	}
	n.store.Inbounds = kept
	return n.apply(tunnels)
}

// UpdateInbound 改端口、备注与启停。
//
// 端口变了 inboundTag 也跟着变（tag 里含端口），所以路由规则要一起重写；
// apply 是整份重建，天然覆盖了这点。
func (n *Native) UpdateInbound(id int, patch InboundPatch, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	finish := n.beginChange()
	defer finish()

	ib := n.store.byID(id)
	if ib == nil {
		return fmt.Errorf("入站 %d 不存在", id)
	}

	if patch.Port != nil && *patch.Port != ib.Port {
		port := *patch.Port
		if port < 1 || port > 65535 {
			return fmt.Errorf("端口 %d 不在合法范围", port)
		}
		for _, other := range n.store.Inbounds {
			if other.ID == id {
				continue
			}
			if other.Port == port {
				return fmt.Errorf("端口 %d 已被入站 %q 占用", port, other.Remark)
			}
		}
		if ib.netOrTCP() == "mkcp" && !portAvailableUDP(port) {
			return fmt.Errorf("UDP 端口 %d 已被占用", port)
		}
		ib.Port = port
	}
	if patch.NameMode != nil {
		if *patch.NameMode != "auto" && *patch.NameMode != "custom" {
			return fmt.Errorf("命名模式无效")
		}
		ib.NameMode = *patch.NameMode
	}
	if patch.Remark != nil {
		if r := strings.TrimSpace(*patch.Remark); r != "" {
			ib.Remark = r
			if patch.NameMode == nil {
				ib.NameMode = "custom"
			}
		}
	}
	if patch.Enable != nil {
		ib.Enable = *patch.Enable
	}
	return n.apply(tunnels)
}

// AddClient 给入站加一个客户端。同一入站上可以有多套凭据，便于分发给不同人。
func (n *Native) AddClient(id int, email string, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	finish := n.beginChange()
	defer finish()

	ib := n.store.byID(id)
	if ib == nil {
		return fmt.Errorf("入站 %d 不存在", id)
	}

	email = strings.TrimSpace(email)
	if email == "" {
		email = fmt.Sprintf("%s-%d-%s", ib.Protocol, ib.Port, randomHex(3))
	}
	for _, c := range ib.Clients {
		if c.Email == email {
			return fmt.Errorf("客户端 %q 已存在", email)
		}
	}

	ib.Clients = append(ib.Clients, nativeClient{
		Email:    email,
		ID:       newUUID(),
		Password: randomHex(8),
		Enable:   true,
		Flow:     visionFlow(ib),
	})
	return n.apply(tunnels)
}

// DeleteClient 摘掉一个客户端。留下最后一个是有意的：
// 没有任何客户端的入站在 Xray 里虽然合法，但谁也连不上，只会让人以为坏了。
func (n *Native) DeleteClient(id int, email string, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	finish := n.beginChange()
	defer finish()

	ib := n.store.byID(id)
	if ib == nil {
		return fmt.Errorf("入站 %d 不存在", id)
	}
	if len(ib.Clients) <= 1 {
		return fmt.Errorf("这是最后一个客户端，删掉就没人能连了")
	}

	kept := make([]nativeClient, 0, len(ib.Clients))
	for _, c := range ib.Clients {
		if c.Email != email {
			kept = append(kept, c)
		}
	}
	if len(kept) == len(ib.Clients) {
		return fmt.Errorf("客户端 %q 不存在", email)
	}
	ib.Clients = kept
	return n.apply(tunnels)
}

// ResetClient 换一套新凭据，旧链接立即失效。
func (n *Native) ResetClient(id int, email string, tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	finish := n.beginChange()
	defer finish()

	ib := n.store.byID(id)
	if ib == nil {
		return fmt.Errorf("入站 %d 不存在", id)
	}
	for i := range ib.Clients {
		if ib.Clients[i].Email == email {
			ib.Clients[i].ID = newUUID()
			ib.Clients[i].Password = randomHex(8)
			return n.apply(tunnels)
		}
	}
	return fmt.Errorf("客户端 %q 不存在", email)
}

// RotateVLESSEncryption 重新生成一对 VLESS Encryption 参数并应用配置。
func (n *Native) RotateVLESSEncryption(id int, tunnels []*Tunnel) (*vlessEncryptionConfig, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	finish := n.beginChange()
	defer finish()

	ib := n.store.byID(id)
	if ib == nil {
		return nil, fmt.Errorf("入站 %d 不存在", id)
	}
	if ib.Protocol != "vless" || ib.VLESSEncryption == nil || !ib.VLESSEncryption.Enabled {
		return nil, fmt.Errorf("该入站未启用 VLESS Encryption")
	}
	if n.proc == nil {
		return nil, fmt.Errorf("Xray 运行时未就绪")
	}
	enc, err := newVLESSEncryption(n.proc.bin, ib.VLESSEncryption.KeyType)
	if err != nil {
		return nil, err
	}
	ib.VLESSEncryption = enc
	if err := n.apply(tunnels); err != nil {
		return nil, err
	}
	return enc, nil
}

// SetVLESSEncryption 打开或关闭一个 VLESS 入站的协议内加密层。
// 打开时如果还没有参数，或切换了密钥类型，会生成新的一对参数。
func (n *Native) SetVLESSEncryption(id int, enabled bool, keyType string, tunnels []*Tunnel) (*vlessEncryptionConfig, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	finish := n.beginChange()
	defer finish()

	ib := n.store.byID(id)
	if ib == nil || ib.Protocol != "vless" {
		return nil, fmt.Errorf("只有 VLESS 入站支持 VLESS Encryption")
	}
	if !enabled {
		ib.VLESSEncryption = nil
		if err := n.apply(tunnels); err != nil {
			return nil, err
		}
		return nil, nil
	}

	kt := strings.ToLower(strings.TrimSpace(keyType))
	if kt == "" && ib.VLESSEncryption != nil {
		kt = ib.VLESSEncryption.KeyType
	}
	if kt == "" {
		kt = "x25519"
	}
	if ib.VLESSEncryption != nil && ib.VLESSEncryption.Enabled && ib.VLESSEncryption.KeyType == kt && ib.VLESSEncryption.ServerDecryption != "" {
		return ib.VLESSEncryption, nil
	}
	if n.proc == nil {
		return nil, fmt.Errorf("Xray 运行时未就绪")
	}
	enc, err := newVLESSEncryption(n.proc.bin, kt)
	if err != nil {
		return nil, err
	}
	ib.VLESSEncryption = enc
	if err := n.apply(tunnels); err != nil {
		return nil, err
	}
	return enc, nil
}

// visionFlow 沿用入站已有客户端的 flow，让新加的客户端与其余保持一致。
func visionFlow(ib *nativeInbound) string {
	for _, c := range ib.Clients {
		if c.Flow != "" {
			return c.Flow
		}
	}
	return ""
}
