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
	return LoadBalancer{
		Name: r.Name, Scheme: r.LoadBalancingScheme, VIP: r.IPAddress, PortRange: port,
		Protocol: r.IPProtocol, Target: lastSeg(r.Target), Region: region, SelfLink: r.SelfLink,
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
