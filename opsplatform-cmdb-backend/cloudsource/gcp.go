package cloudsource

import (
	"context"
	"fmt"
	"time"

	cloudresourcemanager "google.golang.org/api/cloudresourcemanager/v1"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

// GCP 主机适配器：只读列出 Compute Engine 实例（一期只读）。
type GCP struct{ credJSON string }

func (g *GCP) ListInstances(ctx context.Context, projects []string) ([]Instance, error) {
	svc, err := compute.NewService(ctx, option.WithCredentialsJSON([]byte(g.credJSON)),
		option.WithScopes(compute.ComputeReadonlyScope))
	if err != nil {
		return nil, fmt.Errorf("初始化 GCP 客户端失败: %w", err)
	}
	// Resource Manager：取项目显示名（可重命名）；构造失败/无权限则回退用 project id。
	crm, _ := cloudresourcemanager.NewService(ctx, option.WithCredentialsJSON([]byte(g.credJSON)))

	var out []Instance
	mtCache := map[string][2]int{} // zone/machineType -> [vcpu, memMB]
	dtCache := map[string]string{} // disk selfLink -> pd-ssd/pd-standard/...

	for _, proj := range projects {
		projName := proj // 回退：显示名取不到就用 project id
		if crm != nil {
			if p, e := crm.Projects.Get(proj).Context(ctx).Do(); e == nil && p.Name != "" {
				projName = p.Name
			}
		}
		err := svc.Instances.AggregatedList(proj).Pages(ctx, func(page *compute.InstanceAggregatedList) error {
			for _, scoped := range page.Items {
				for _, inst := range scoped.Instances {
					it := Instance{
						InstanceID:  fmt.Sprint(inst.Id),
						Name:        inst.Name,
						Project:     proj,
						ProjectName: projName,
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
						}
						for _, ac := range ni.AccessConfigs {
							if ac.NatIP != "" && it.ExternalIP == "" {
								it.ExternalIP = ac.NatIP
							}
						}
					}
					// vCPU / 内存：machineTypes.get（按 zone+type 缓存）
					key := it.Zone + "/" + it.MachineType
					if v, ok := mtCache[key]; ok {
						it.VCPU, it.MemMB = v[0], v[1]
					} else if mt, e := svc.MachineTypes.Get(proj, it.Zone, it.MachineType).Context(ctx).Do(); e == nil {
						it.VCPU, it.MemMB = int(mt.GuestCpus), int(mt.MemoryMb)
						mtCache[key] = [2]int{it.VCPU, it.MemMB}
					}
					// 磁盘：逐块
					for _, d := range inst.Disks {
						disk := Disk{SizeGB: int(d.DiskSizeGb), IsBoot: d.Boot, Name: lastSeg(d.DeviceName)}
						if disk.Name == "" {
							disk.Name = lastSeg(d.Source)
						}
						disk.Type = g.diskType(ctx, svc, proj, it.Zone, d, dtCache)
						it.Disks = append(it.Disks, disk)
					}
					it.OS = bootOS(inst)
					out = append(out, it)
				}
			}
			return nil
		})
		if err != nil {
			return out, fmt.Errorf("列出 project %s 实例失败: %w", proj, err)
		}
	}
	return out, nil
}

// diskType 尽力取磁盘类型（pd-ssd/pd-standard/...）；取不到回退 pd-balanced。
func (g *GCP) diskType(ctx context.Context, svc *compute.Service, proj, zone string, d *compute.AttachedDisk, cache map[string]string) string {
	if d.Source == "" {
		return "pd-balanced"
	}
	if t, ok := cache[d.Source]; ok {
		return t
	}
	t := "pd-balanced"
	if dk, err := svc.Disks.Get(proj, zone, lastSeg(d.Source)).Context(ctx).Do(); err == nil && dk.Type != "" {
		t = lastSeg(dk.Type)
	}
	cache[d.Source] = t
	return t
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
