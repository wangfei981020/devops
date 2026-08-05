package cloudsource

import (
	"context"
	"fmt"
	"strings"

	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

// ListNetwork 只读列出单个 project 的网络资源：VPC/子网/防火墙/静态IP/负载均衡（转发规则）。
func (g *GCP) ListNetwork(ctx context.Context, projectID string) (*NetworkResources, error) {
	svc, err := compute.NewService(ctx, option.WithCredentialsJSON([]byte(g.credJSON)),
		option.WithScopes(compute.ComputeReadonlyScope))
	if err != nil {
		return nil, fmt.Errorf("初始化 GCP 客户端失败: %w", err)
	}
	lim := limiterFor(projectID)
	nr := &NetworkResources{}

	// VPC
	if err := g.retry(ctx, lim, func() error {
		return svc.Networks.List(projectID).Pages(ctx, func(page *compute.NetworkList) error {
			for _, n := range page.Items {
				mode := "custom"
				if n.AutoCreateSubnetworks {
					mode = "auto"
				}
				nr.Networks = append(nr.Networks, Network{Name: n.Name, Mode: mode, SelfLink: n.SelfLink})
			}
			return nil
		})
	}); err != nil {
		return nil, fmt.Errorf("列 VPC 失败: %w", err)
	}

	// 子网
	if err := g.retry(ctx, lim, func() error {
		return svc.Subnetworks.AggregatedList(projectID).Pages(ctx, func(page *compute.SubnetworkAggregatedList) error {
			for _, scoped := range page.Items {
				for _, s := range scoped.Subnetworks {
					nr.Subnets = append(nr.Subnets, Subnet{
						Name: s.Name, Network: lastSeg(s.Network), Region: lastSeg(s.Region),
						CIDR: s.IpCidrRange, Gateway: s.GatewayAddress, SelfLink: s.SelfLink,
					})
				}
			}
			return nil
		})
	}); err != nil {
		return nil, fmt.Errorf("列子网失败: %w", err)
	}

	// 防火墙
	if err := g.retry(ctx, lim, func() error {
		return svc.Firewalls.List(projectID).Pages(ctx, func(page *compute.FirewallList) error {
			for _, f := range page.Items {
				fw := Firewall{
					Name: f.Name, Network: lastSeg(f.Network), Direction: f.Direction,
					Priority: int(f.Priority), Disabled: f.Disabled, SelfLink: f.SelfLink,
					SourceRanges: strings.Join(f.SourceRanges, ","),
					TargetTags:   strings.Join(f.TargetTags, ","),
				}
				if len(f.Allowed) > 0 {
					fw.Action = "allow"
					fw.Protocols = fwProtocols(f.Allowed)
				} else {
					fw.Action = "deny"
					fw.Protocols = fwProtocolsDenied(f.Denied)
				}
				if fw.Direction == "" {
					fw.Direction = "INGRESS"
				}
				nr.Firewalls = append(nr.Firewalls, fw)
			}
			return nil
		})
	}); err != nil {
		return nil, fmt.Errorf("列防火墙失败: %w", err)
	}

	// 静态IP：区域 + 全局
	if err := g.retry(ctx, lim, func() error {
		return svc.Addresses.AggregatedList(projectID).Pages(ctx, func(page *compute.AddressAggregatedList) error {
			for _, scoped := range page.Items {
				for _, a := range scoped.Addresses {
					nr.Addresses = append(nr.Addresses, addrOf(a))
				}
			}
			return nil
		})
	}); err != nil {
		return nil, fmt.Errorf("列静态IP失败: %w", err)
	}
	_ = g.retry(ctx, lim, func() error {
		return svc.GlobalAddresses.List(projectID).Pages(ctx, func(page *compute.AddressList) error {
			for _, a := range page.Items {
				nr.Addresses = append(nr.Addresses, addrOf(a))
			}
			return nil
		})
	})

	// 负载均衡（转发规则）：区域 + 全局
	if err := g.retry(ctx, lim, func() error {
		return svc.ForwardingRules.AggregatedList(projectID).Pages(ctx, func(page *compute.ForwardingRuleAggregatedList) error {
			for _, scoped := range page.Items {
				for _, r := range scoped.ForwardingRules {
					nr.LoadBalancers = append(nr.LoadBalancers, lbOf(r))
				}
			}
			return nil
		})
	}); err != nil {
		return nil, fmt.Errorf("列负载均衡失败: %w", err)
	}
	_ = g.retry(ctx, lim, func() error {
		return svc.GlobalForwardingRules.List(projectID).Pages(ctx, func(page *compute.ForwardingRuleList) error {
			for _, r := range page.Items {
				nr.LoadBalancers = append(nr.LoadBalancers, lbOf(r))
			}
			return nil
		})
	})

	// 追溯 LB 后端实例（best-effort，失败不影响上面的结果）
	g.resolveLBBackends(ctx, svc, projectID, nr.LoadBalancers, lim)

	return nr, nil
}

func addrOf(a *compute.Address) Address {
	region := "global"
	if a.Region != "" {
		region = lastSeg(a.Region)
	}
	users := make([]string, 0, len(a.Users))
	for _, u := range a.Users {
		users = append(users, lastSeg(u))
	}
	return Address{
		Name: a.Name, Address: a.Address, Type: a.AddressType, Status: a.Status,
		Region: region, Users: strings.Join(users, ","), SelfLink: a.SelfLink,
	}
}

func lbOf(r *compute.ForwardingRule) LoadBalancer {
	region := "global"
	if r.Region != "" {
		region = lastSeg(r.Region)
	}
	port := r.PortRange
	if port == "" && len(r.Ports) > 0 {
		port = strings.Join(r.Ports, ",")
	}
	// ⚠️ 转发规则的后端指向有**两个互斥字段**，只读 Target 会漏掉一大半。
	//
	//	target          → targetPool / target*Proxy（L4 网络LB、L7）
	//	backendService  → 内部直通 LB（INTERNAL）和新版外部直通网络LB
	//
	//	原来只取了 target，于是所有 INTERNAL 转发规则的 Target 都是空串，
	//	在 resolveLBBackends 里直接命中 `Target == "" → BackendState=none`，
	//	被判成**"确认没有后端"**。生产 81 条里 48 条 none，绝大多数是这么来的——
	//	g32-prod-tidb-ilb、doris-fe 这些明摆着有后端的内部 LB 全在里面。
	//	把"没查"渲染成"没有"，比留空更误导（CMDB-042）。
	target := lastSeg(r.Target)
	if target == "" {
		target = lastSeg(r.BackendService)
	}
	return LoadBalancer{
		Name: r.Name, Scheme: r.LoadBalancingScheme, VIP: r.IPAddress, PortRange: port,
		Protocol: r.IPProtocol, Target: target, Region: region, SelfLink: r.SelfLink,
	}
}

// fwProtocols 把 allowed 规则拼成 "tcp:22,80;udp:53;icmp"
func fwProtocols(allowed []*compute.FirewallAllowed) string {
	var parts []string
	for _, a := range allowed {
		if len(a.Ports) > 0 {
			parts = append(parts, a.IPProtocol+":"+strings.Join(a.Ports, ","))
		} else {
			parts = append(parts, a.IPProtocol)
		}
	}
	return strings.Join(parts, ";")
}

func fwProtocolsDenied(denied []*compute.FirewallDenied) string {
	var parts []string
	for _, a := range denied {
		if len(a.Ports) > 0 {
			parts = append(parts, a.IPProtocol+":"+strings.Join(a.Ports, ","))
		} else {
			parts = append(parts, a.IPProtocol)
		}
	}
	return strings.Join(parts, ";")
}
