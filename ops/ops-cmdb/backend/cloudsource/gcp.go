package cloudsource

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

// GCP 主机适配器：只读列出单个 project 的 Compute Engine 实例（一期只读）。
// 凭据 per-project；带客户端限流 + 429/rateLimitExceeded 指数退避，避免撞配额。
type GCP struct{ credJSON string }

func (g *GCP) ListInstances(ctx context.Context, projectID string) ([]Instance, error) {
	svc, err := compute.NewService(ctx, option.WithCredentialsJSON([]byte(g.credJSON)),
		option.WithScopes(compute.ComputeReadonlyScope))
	if err != nil {
		return nil, fmt.Errorf("初始化 GCP 客户端失败: %w", err)
	}
	lim := limiterFor(projectID)

	// 磁盘一次性拿全 project（disks.aggregatedList），建 selfLink->type / selfLink->镜像 索引，避免逐块 disks.get。
	diskType := map[string]string{}
	diskImage := map[string]string{}
	if err := g.retry(ctx, lim, func() error {
		return svc.Disks.AggregatedList(projectID).Pages(ctx, func(page *compute.DiskAggregatedList) error {
			for _, scoped := range page.Items {
				for _, d := range scoped.Disks {
					diskType[d.SelfLink] = lastSeg(d.Type)
					if d.SourceImage != "" {
						diskImage[d.SelfLink] = lastSeg(d.SourceImage)
					}
				}
			}
			return nil
		})
	}); err != nil {
		return nil, fmt.Errorf("列出 project %s 磁盘失败: %w", projectID, err)
	}

	// 机型一次性拿全 project（machineTypes.aggregatedList），建 zone/type->[vcpu,memMB] 索引。
	mt := map[string][2]int{}
	if err := g.retry(ctx, lim, func() error {
		return svc.MachineTypes.AggregatedList(projectID).Pages(ctx, func(page *compute.MachineTypeAggregatedList) error {
			for _, scoped := range page.Items {
				for _, m := range scoped.MachineTypes {
					mt[lastSeg(m.Zone)+"/"+m.Name] = [2]int{int(m.GuestCpus), int(m.MemoryMb)}
				}
			}
			return nil
		})
	}); err != nil {
		return nil, fmt.Errorf("列出 project %s 机型失败: %w", projectID, err)
	}

	var out []Instance
	err = g.retry(ctx, lim, func() error {
		out = out[:0]
		return svc.Instances.AggregatedList(projectID).Pages(ctx, func(page *compute.InstanceAggregatedList) error {
			for _, scoped := range page.Items {
				for _, inst := range scoped.Instances {
					it := Instance{
						InstanceID:  fmt.Sprint(inst.Id),
						Name:        inst.Name,
						Project:     projectID,
						Status:      inst.Status,
						SelfLink:    inst.SelfLink,
						Labels:      inst.Labels,
						Zone:        lastSeg(inst.Zone),
						MachineType: lastSeg(inst.MachineType),
					}
					it.Region = regionOfZone(it.Zone)
					if t, e := time.Parse(time.RFC3339, inst.CreationTimestamp); e == nil {
						it.CreatedAt = &t
					}
					for _, ni := range inst.NetworkInterfaces {
						if it.InternalIP == "" {
							it.InternalIP = ni.NetworkIP
							it.VPC = lastSeg(ni.Network)
							it.Subnet = lastSeg(ni.Subnetwork)
						}
						for _, ac := range ni.AccessConfigs {
							if ac.NatIP != "" && it.ExternalIP == "" {
								it.ExternalIP = ac.NatIP
							}
						}
					}
					// GCP 只读技术字段
					it.Hostname = inst.Hostname
					it.CPUPlatform = inst.CpuPlatform
					it.DeletionProtection = inst.DeletionProtection
					if inst.Scheduling != nil {
						it.Preemptible = inst.Scheduling.Preemptible || inst.Scheduling.ProvisioningModel == "SPOT"
					}
					if inst.Tags != nil {
						it.NetworkTags = inst.Tags.Items
					}
					for _, sa := range inst.ServiceAccounts {
						if sa.Email != "" {
							it.ServiceAccounts = append(it.ServiceAccounts, sa.Email)
						}
					}
					for _, d := range inst.Disks {
						if d.Boot {
							if img, ok := diskImage[d.Source]; ok {
								it.Image = img
							}
						}
					}
					if v, ok := mt[it.Zone+"/"+it.MachineType]; ok {
						it.VCPU, it.MemMB = v[0], v[1]
					} else if cpu, mem, ok := parseCustomMachineType(it.MachineType); ok {
						// 自定义机型不在 aggregatedList 里，但名字自带规格
						it.VCPU, it.MemMB = cpu, mem
						mt[it.Zone+"/"+it.MachineType] = [2]int{cpu, mem}
					} else if m, e := g.machineTypeGet(ctx, svc, lim, projectID, it.Zone, it.MachineType); e == nil {
						it.VCPU, it.MemMB = m[0], m[1]
						mt[it.Zone+"/"+it.MachineType] = m
					}
					for _, d := range inst.Disks {
						disk := Disk{SizeGB: int(d.DiskSizeGb), IsBoot: d.Boot, Name: lastSeg(d.DeviceName)}
						if disk.Name == "" {
							disk.Name = lastSeg(d.Source)
						}
						if t, ok := diskType[d.Source]; ok && t != "" {
							disk.Type = t
						} else {
							disk.Type = "pd-balanced"
						}
						it.Disks = append(it.Disks, disk)
					}
					it.OS = bootOS(inst)
					out = append(out, it)
				}
			}
			return nil
		})
	})
	if err != nil {
		return out, fmt.Errorf("列出 project %s 实例失败: %w", projectID, err)
	}
	return out, nil
}

// retry 客户端限流 + 遇 429 / rateLimitExceeded 指数退避重试（最多 5 次：1s→2s→4s→8s）。
func (g *GCP) retry(ctx context.Context, lim *limiter, fn func() error) error {
	backoff := time.Second
	for attempt := 0; ; attempt++ {
		if err := lim.wait(ctx); err != nil {
			return err
		}
		err := fn()
		if err == nil || !isRateLimit(err) || attempt >= 4 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}

// isRateLimit 判断是否 GCP 限流类错误（429，或 403 rateLimitExceeded）。
func isRateLimit(err error) bool {
	var ae *googleapi.Error
	if errors.As(err, &ae) {
		if ae.Code == 429 {
			return true
		}
		if ae.Code == 403 {
			for _, e := range ae.Errors {
				if e.Reason == "rateLimitExceeded" || e.Reason == "userRateLimitExceeded" {
					return true
				}
			}
		}
	}
	return false
}

// parseCustomMachineType 从自定义机型名解析 vCPU / 内存(MB)。
// 名字形如 custom-8-32768 / n2-custom-8-32768 / e2-custom-2-4096 / custom-8-32768-ext（扩展内存）。
// aggregatedList 不返回自定义机型，但名字里就含规格，零额外请求。
func parseCustomMachineType(name string) (vcpu, memMB int, ok bool) {
	i := strings.Index(name, "custom-")
	if i < 0 {
		return 0, 0, false
	}
	parts := strings.Split(name[i+len("custom-"):], "-") // "8","32768"[,"ext"]
	if len(parts) < 2 {
		return 0, 0, false
	}
	cpu, e1 := strconv.Atoi(parts[0])
	mem, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil || cpu <= 0 || mem <= 0 {
		return 0, 0, false
	}
	return cpu, mem, true
}

// machineTypeGet 兜底：aggregatedList 与名字解析都拿不到时，逐个 machineTypes.get（含限流退避、可返回自定义机型规格）。
func (g *GCP) machineTypeGet(ctx context.Context, svc *compute.Service, lim *limiter, projectID, zone, name string) ([2]int, error) {
	var out [2]int
	err := g.retry(ctx, lim, func() error {
		m, e := svc.MachineTypes.Get(projectID, zone, name).Context(ctx).Do()
		if e != nil {
			return e
		}
		out = [2]int{int(m.GuestCpus), int(m.MemoryMb)}
		return nil
	})
	return out, err
}

// bootOS 尽力从启动盘 licenses 猜操作系统；取不到留空。
func bootOS(inst *compute.Instance) string {
	for _, d := range inst.Disks {
		if d.Boot {
			for _, lic := range d.Licenses {
				return lastSeg(lic)
			}
		}
	}
	return ""
}
