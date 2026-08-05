package cloudsource

import (
	"context"
	"log"
	"strings"

	compute "google.golang.org/api/compute/v1"
)

// resolveLBBackends 尽力把每个转发规则追溯到后端实例（option A：实例名 + 实例组 + zone）。
// 覆盖：L4 网络LB(targetPool→instances)、L4 内部LB(backendService→实例组)、L7(target*Proxy→urlMap→backendService→实例组)。
// 全程防御式：任何一跳失败只影响该 LB 的后端为空，绝不影响 LB 本身与其它网络资源的同步。
func (g *GCP) resolveLBBackends(ctx context.Context, svc *compute.Service, projectID string, lbs []LoadBalancer, lim *limiter) {
	if len(lbs) == 0 {
		return
	}
	pools := map[string][]string{}          // targetPool selfLink -> 实例URL
	bsGroups := map[string][]string{}       // backendService selfLink -> 实例组/NEG URL
	proxyURLMap := map[string]string{}      // http/https proxy selfLink -> urlMap URL
	proxyService := map[string]string{}     // tcp/ssl proxy selfLink -> backendService URL
	urlMapServices := map[string][]string{} // urlMap selfLink -> backendService URL 列表
	groupInstances := map[string][]string{} // 实例组/NEG URL -> 实例名（懒加载缓存）

	// try 包一层：失败不中断，但打日志（不再静默吞错）。
	// 同时记下"这次追溯是不是残缺的"——只打日志不够，界面上还得能区分
	// 「真的没后端」和「没追到」，否则 81 个 LB 全空会被当成"LB 都挂空了"。
	degraded := false
	try := func(label string, fn func() error) {
		if err := g.retry(ctx, lim, fn); err != nil {
			log.Printf("[lb-backend] WARN 项目 %s 拉取 %s 失败: %v", projectID, label, err)
			degraded = true
		}
	}

	// targetPools（区域+全局，aggregated 覆盖）
	try("targetPools", func() error {
		return svc.TargetPools.AggregatedList(projectID).Pages(ctx, func(page *compute.TargetPoolAggregatedList) error {
			for _, sc := range page.Items {
				for _, tp := range sc.TargetPools {
					pools[tp.SelfLink] = tp.Instances
				}
			}
			return nil
		})
	})
	// backendServices（aggregated 含区域+全局）
	try("backendServices", func() error {
		return svc.BackendServices.AggregatedList(projectID).Pages(ctx, func(page *compute.BackendServiceAggregatedList) error {
			for _, sc := range page.Items {
				for _, bs := range sc.BackendServices {
					var groups []string
					for _, b := range bs.Backends {
						if b.Group != "" {
							groups = append(groups, b.Group)
						}
					}
					bsGroups[bs.SelfLink] = groups
				}
			}
			return nil
		})
	})
	// urlMaps（aggregated 含区域+全局）
	try("urlMaps", func() error {
		return svc.UrlMaps.AggregatedList(projectID).Pages(ctx, func(page *compute.UrlMapsAggregatedList) error {
			for _, sc := range page.Items {
				for _, um := range sc.UrlMaps {
					var svcs []string
					if um.DefaultService != "" {
						svcs = append(svcs, um.DefaultService)
					}
					for _, pm := range um.PathMatchers {
						if pm.DefaultService != "" {
							svcs = append(svcs, pm.DefaultService)
						}
						for _, pr := range pm.PathRules {
							if pr.Service != "" {
								svcs = append(svcs, pr.Service)
							}
						}
					}
					urlMapServices[um.SelfLink] = svcs
				}
			}
			return nil
		})
	})
	// target http/https proxies -> urlMap
	try("targetHttpProxies", func() error {
		return svc.TargetHttpProxies.AggregatedList(projectID).Pages(ctx, func(page *compute.TargetHttpProxyAggregatedList) error {
			for _, sc := range page.Items {
				for _, p := range sc.TargetHttpProxies {
					proxyURLMap[p.SelfLink] = p.UrlMap
				}
			}
			return nil
		})
	})
	try("targetHttpsProxies", func() error {
		return svc.TargetHttpsProxies.AggregatedList(projectID).Pages(ctx, func(page *compute.TargetHttpsProxyAggregatedList) error {
			for _, sc := range page.Items {
				for _, p := range sc.TargetHttpsProxies {
					proxyURLMap[p.SelfLink] = p.UrlMap
				}
			}
			return nil
		})
	})
	// target tcp/ssl proxies（全局）-> backendService
	try("targetTcpProxies", func() error {
		return svc.TargetTcpProxies.List(projectID).Pages(ctx, func(page *compute.TargetTcpProxyList) error {
			for _, p := range page.Items {
				proxyService[p.SelfLink] = p.Service
			}
			return nil
		})
	})
	try("targetSslProxies", func() error {
		return svc.TargetSslProxies.List(projectID).Pages(ctx, func(page *compute.TargetSslProxyList) error {
			for _, p := range page.Items {
				proxyService[p.SelfLink] = p.Service
			}
			return nil
		})
	})

	// membersOf：实例组/NEG -> 实例名（懒加载，只查被 LB 引用到的）。
	// 支持：zonal/regional 实例组 + zonal NEG（GKE 容器原生后端常用；GCE_VM 端点带实例名）。
	membersOf := func(groupURL string) []string {
		if v, ok := groupInstances[groupURL]; ok {
			return v
		}
		scope, kind := scopeOfURL(groupURL) // kind: zone/region
		name := lastSeg(groupURL)
		var insts []string
		switch {
		case scope == "" || name == "":
			// 无法解析 scope，跳过
		case strings.Contains(groupURL, "/networkEndpointGroups/"):
			// NEG：列端点，取 GCE_VM 型端点的实例名（serverless/PSC/互联网型无实例）
			if kind == "region" {
				try("regionNEG "+name, func() error {
					return svc.RegionNetworkEndpointGroups.ListNetworkEndpoints(projectID, scope, name).Pages(ctx, func(page *compute.NetworkEndpointGroupsListNetworkEndpoints) error {
						for _, e := range page.Items {
							if e.NetworkEndpoint != nil && e.NetworkEndpoint.Instance != "" {
								insts = append(insts, e.NetworkEndpoint.Instance)
							}
						}
						return nil
					})
				})
			} else {
				try("zonalNEG "+name, func() error {
					return svc.NetworkEndpointGroups.ListNetworkEndpoints(projectID, scope, name, &compute.NetworkEndpointGroupsListEndpointsRequest{}).Pages(ctx, func(page *compute.NetworkEndpointGroupsListNetworkEndpoints) error {
						for _, e := range page.Items {
							if e.NetworkEndpoint != nil && e.NetworkEndpoint.Instance != "" {
								insts = append(insts, e.NetworkEndpoint.Instance)
							}
						}
						return nil
					})
				})
			}
		case kind == "region":
			try("regionInstanceGroup "+name, func() error {
				return svc.RegionInstanceGroups.ListInstances(projectID, scope, name, &compute.RegionInstanceGroupsListInstancesRequest{}).Pages(ctx, func(page *compute.RegionInstanceGroupsListInstances) error {
					for _, it := range page.Items {
						insts = append(insts, it.Instance)
					}
					return nil
				})
			})
		default:
			try("instanceGroup "+name, func() error {
				return svc.InstanceGroups.ListInstances(projectID, scope, name, &compute.InstanceGroupsListInstancesRequest{}).Pages(ctx, func(page *compute.InstanceGroupsListInstances) error {
					for _, it := range page.Items {
						insts = append(insts, it.Instance)
					}
					return nil
				})
			})
		}
		groupInstances[groupURL] = insts
		return insts
	}

	for i := range lbs {
		lb := &lbs[i]
		seen := map[string]bool{}
		var backends []LBBackend
		addInstance := func(instURL, groupURL string) {
			name := lastSeg(instURL)
			if name == "" || seen[name] {
				return
			}
			seen[name] = true
			// 实例组成员的 instURL 带 zone；NEG 成员是裸实例名(无 zone)，退回用组/NEG 的 zone
			scope, _ := scopeOfURL(instURL)
			if scope == "" {
				scope, _ = scopeOfURL(groupURL)
			}
			backends = append(backends, LBBackend{Instance: name, Group: lastSeg(groupURL), Zone: scope})
		}

		// lb.Target 存的是 lastSeg（名字），按名字在各 map 里反查其关联的
		// targetPool / backendService selfLink（单 project 内名字唯一，足够）。
		var poolURLs, serviceURLs []string
		classifyTarget(lb.Target, pools, bsGroups, proxyURLMap, proxyService, urlMapServices, &poolURLs, &serviceURLs)

		for _, purl := range poolURLs {
			for _, inst := range pools[purl] {
				addInstance(inst, "")
			}
		}
		for _, surl := range serviceURLs {
			for _, gp := range bsGroups[surl] {
				for _, inst := range membersOf(gp) {
					addInstance(inst, gp)
				}
			}
		}
		lb.Backends = backends
		switch {
		case len(backends) > 0:
			lb.BackendState = "ok"
		case degraded:
			// 上游某一跳拉失败了：这个 LB 到底有没有后端**我们不知道**，
			// 绝不能显示成"无后端"
			lb.BackendState = "unresolved"
		case lb.Target == "":
			lb.BackendState = "none"
		case len(poolURLs) == 0 && len(serviceURLs) == 0:
			// 有 target 但既不是 targetPool 也不是 backendService——
			// 多半是服务型 NEG（GKE Service/Ingress 直连 Pod），当前追溯不了
			lb.BackendState = "unsupported"
		default:
			lb.BackendState = "none"
		}
		// 有 target 却一个后端都没追到 → 打日志，便于排查（服务型后端/未知类型/权限等）
		if len(backends) == 0 && lb.Target != "" {
			log.Printf("[lb-backend] WARN LB %s (project=%s, target=%s) 未追溯到后端实例：命中 targetPool=%d backendService=%d（可能是无实例后端/服务型NEG，或状态不支持）",
				lb.Name, projectID, lb.Target, len(poolURLs), len(serviceURLs))
		}
	}
}

// classifyTarget 按 target 名字在各 map 里反查，产出关联的 targetPool / backendService 的 selfLink 列表。
func classifyTarget(target string,
	pools, bsGroups map[string][]string,
	proxyURLMap, proxyService map[string]string,
	urlMapServices map[string][]string,
	outPools, outServices *[]string) {
	if target == "" {
		return
	}
	// 1) 直接命中 targetPool
	for sl := range pools {
		if lastSeg(sl) == target {
			*outPools = append(*outPools, sl)
		}
	}
	// 2) 直接命中 backendService
	for sl := range bsGroups {
		if lastSeg(sl) == target {
			*outServices = append(*outServices, sl)
		}
	}
	// 3) http/https proxy -> urlMap -> services
	for sl, um := range proxyURLMap {
		if lastSeg(sl) == target {
			for _, svcURL := range urlMapServices[um] {
				*outServices = append(*outServices, svcURL)
			}
		}
	}
	// 4) tcp/ssl proxy -> service
	for sl, svcURL := range proxyService {
		if lastSeg(sl) == target && svcURL != "" {
			*outServices = append(*outServices, svcURL)
		}
	}
}

// scopeOfURL 从 GCP 资源 URL 里取 zone 或 region 名，kind 返回 "zone"/"region"/""。
func scopeOfURL(u string) (scope, kind string) {
	if i := strings.Index(u, "/zones/"); i >= 0 {
		rest := u[i+len("/zones/"):]
		return firstSeg(rest), "zone"
	}
	if i := strings.Index(u, "/regions/"); i >= 0 {
		rest := u[i+len("/regions/"):]
		return firstSeg(rest), "region"
	}
	return "", ""
}

func firstSeg(s string) string {
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return s[:i]
	}
	return s
}
