package cloudsource

import (
	"context"
	"log"
	"strings"

	crm "google.golang.org/api/cloudresourcemanager/v1"
	dns "google.golang.org/api/dns/v1"
	"google.golang.org/api/option"
)

// GCP IAM 与 Cloud DNS 只读采集。
//
// ⚠️ 本文件的响应字段映射是按 Google API 官方文档写的，**未经真实凭证验证**
// （本地 cloud_accounts 没有凭据，采不到任何数据）。因此这里的原则是：
// 取不到就明确报错并打日志说清是哪一步、什么错，绝不静默返回空——
// 空结果被当成「没有风险」比报错危险得多。部署到有真实凭证的环境后，
// 先看 [gcp-iam] / [gcp-dns] 开头的日志再采信数据。

// IAMBinding 一条权限绑定，已拆成「谁」+「什么角色」。
type IAMBinding struct {
	Role       string
	MemberType string // user / serviceAccount / group / domain / allUsers / allAuthenticatedUsers
	Member     string // 去掉类型前缀后的标识
	Severity   string
	Issue      string
}

// DNSZone GCP Cloud DNS 托管区。
type DNSZone struct {
	Name        string // GCP 里的区名
	DNSName     string // 根域名（带尾点）
	Visibility  string
	NameServers []string
	Records     []DNSRecordRR
}

// DNSRecordRR 一条解析记录。
type DNSRecordRR struct {
	Name    string
	Type    string
	TTL     int
	RRDatas []string
}

// wideRoles 权限过宽的预置角色。给这些角色等于给了改动/删除生产资源的能力。
var wideRoles = map[string]string{
	"roles/owner":                           "项目所有者：可做任何操作，包括改 IAM 把权限给别人",
	"roles/editor":                          "项目编辑者：可改删几乎所有资源，仅不能改 IAM",
	"roles/iam.securityAdmin":               "可修改 IAM 策略，等于能自行提权",
	"roles/iam.serviceAccountKeyAdmin":      "可创建服务账号密钥，密钥外泄即长期凭证外泄",
	"roles/resourcemanager.projectIamAdmin": "可修改项目 IAM，等于能自行提权",
}

// splitMember 把 "user:a@b.com" 拆成类型与标识。
// allUsers / allAuthenticatedUsers 没有冒号，单独处理——它们恰恰是最危险的两个。
func splitMember(m string) (typ, id string) {
	switch m {
	case "allUsers":
		return "allUsers", ""
	case "allAuthenticatedUsers":
		return "allAuthenticatedUsers", ""
	}
	if i := strings.Index(m, ":"); i > 0 {
		return m[:i], m[i+1:]
	}
	return "unknown", m
}

// judgeBinding 判定一条绑定的风险。
//
// 分级依据是「拿到这个能干什么」，不是角色名字听起来吓不吓人：
// allUsers 意味着互联网上任何人，无论给什么角色都是最高级别。
func judgeBinding(role, memberType, member string) (severity, issue string) {
	switch memberType {
	case "allUsers":
		return "critical", "授予了 allUsers——互联网上任何人都拥有 " + role + " 权限，无需登录"
	case "allAuthenticatedUsers":
		return "critical", "授予了 allAuthenticatedUsers——任何持有 Google 账号的人都拥有 " + role + " 权限"
	}
	if why, ok := wideRoles[role]; ok {
		sev := "high"
		if role == "roles/editor" {
			sev = "medium" // editor 极其常见，判 high 会淹掉真正该看的 owner
		}
		return sev, "权限过宽：" + why
	}
	// 外部个人邮箱持有项目权限：离职/账号失窃时无法通过公司目录统一回收
	if memberType == "user" && (strings.HasSuffix(member, "@gmail.com") ||
		strings.HasSuffix(member, "@qq.com") || strings.HasSuffix(member, "@163.com")) {
		return "medium", "外部个人邮箱账号持有项目权限，无法随公司目录统一回收"
	}
	return "", ""
}

// ListIAM 读取项目的 IAM 策略。只读：getIamPolicy 不改变任何东西。
func (g *GCP) ListIAM(ctx context.Context, projectID string) ([]IAMBinding, error) {
	svc, err := crm.NewService(ctx, option.WithCredentialsJSON([]byte(g.credJSON)), option.WithScopes(crm.CloudPlatformReadOnlyScope))
	if err != nil {
		log.Printf("[gcp-iam] ERROR project=%s 创建 cloudresourcemanager 客户端失败: %v", projectID, err)
		return nil, err
	}
	lim := limiterFor(projectID)
	var policy *crm.Policy
	err = g.retry(ctx, lim, func() error {
		p, e := svc.Projects.GetIamPolicy(projectID, &crm.GetIamPolicyRequest{}).Context(ctx).Do()
		if e != nil {
			return e
		}
		policy = p
		return nil
	})
	if err != nil {
		// 最常见的失败是缺 resourcemanager.projects.getIamPolicy 权限——把这点写进日志，
		// 否则排查时只看到一个 403 不知道该加什么角色。
		log.Printf("[gcp-iam] ERROR project=%s 取 IAM 策略失败: %v（若为 403，服务账号需要 roles/iam.securityReviewer 或 roles/viewer）", projectID, err)
		return nil, err
	}
	if policy == nil {
		log.Printf("[gcp-iam] WARN project=%s 返回空策略——这不正常，任何项目至少有一条绑定，请确认凭据对应的项目是否正确", projectID)
		return nil, nil
	}

	out := make([]IAMBinding, 0, len(policy.Bindings)*2)
	for _, b := range policy.Bindings {
		if b == nil {
			continue
		}
		for _, m := range b.Members {
			typ, id := splitMember(m)
			sev, issue := judgeBinding(b.Role, typ, id)
			out = append(out, IAMBinding{Role: b.Role, MemberType: typ, Member: id, Severity: sev, Issue: issue})
		}
	}
	log.Printf("[gcp-iam] project=%s 取到 %d 条绑定（%d 个角色）", projectID, len(out), len(policy.Bindings))
	return out, nil
}

// ListDNS 读取 Cloud DNS 托管区与解析记录。
func (g *GCP) ListDNS(ctx context.Context, projectID string) ([]DNSZone, error) {
	svc, err := dns.NewService(ctx, option.WithCredentialsJSON([]byte(g.credJSON)), option.WithScopes(dns.NdevClouddnsReadonlyScope))
	if err != nil {
		log.Printf("[gcp-dns] ERROR project=%s 创建 dns 客户端失败: %v", projectID, err)
		return nil, err
	}
	lim := limiterFor(projectID)

	var zones []*dns.ManagedZone
	err = g.retry(ctx, lim, func() error {
		zones = zones[:0]
		return svc.ManagedZones.List(projectID).Pages(ctx, func(page *dns.ManagedZonesListResponse) error {
			zones = append(zones, page.ManagedZones...)
			return nil
		})
	})
	if err != nil {
		log.Printf("[gcp-dns] ERROR project=%s 列托管区失败: %v（若为 403，服务账号需要 roles/dns.reader；若为 SERVICE_DISABLED，该项目未启用 Cloud DNS API）", projectID, err)
		return nil, err
	}
	if len(zones) == 0 {
		// 空是合法的（该项目可能确实不用 Cloud DNS），但要说出来，
		// 免得「查不到解析」被当成「解析不存在」。
		log.Printf("[gcp-dns] project=%s 没有托管区——该项目可能不使用 Cloud DNS（解析可能在 Cloudflare 或别处）", projectID)
		return nil, nil
	}

	out := make([]DNSZone, 0, len(zones))
	for _, z := range zones {
		if z == nil {
			continue
		}
		dz := DNSZone{Name: z.Name, DNSName: z.DnsName, Visibility: z.Visibility, NameServers: z.NameServers}
		var sets []*dns.ResourceRecordSet
		e := g.retry(ctx, lim, func() error {
			sets = sets[:0]
			return svc.ResourceRecordSets.List(projectID, z.Name).Pages(ctx, func(page *dns.ResourceRecordSetsListResponse) error {
				sets = append(sets, page.Rrsets...)
				return nil
			})
		})
		if e != nil {
			// 单个区取不到不影响其它区，但必须记下来——否则这个区的记录会静默缺失
			log.Printf("[gcp-dns] WARN project=%s zone=%s 取解析记录失败: %v（该区记录本次缺失）", projectID, z.Name, e)
			out = append(out, dz)
			continue
		}
		for _, rs := range sets {
			if rs == nil {
				continue
			}
			dz.Records = append(dz.Records, DNSRecordRR{
				Name: rs.Name, Type: rs.Type, TTL: int(rs.Ttl), RRDatas: rs.Rrdatas,
			})
		}
		out = append(out, dz)
	}
	log.Printf("[gcp-dns] project=%s 取到 %d 个托管区", projectID, len(out))
	return out, nil
}
