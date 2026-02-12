package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// K8sCluster K8s集群配置
type K8sCluster struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kubeconfig string `json:"kubeconfig"` // kubeconfig 文件路径
	Context    string `json:"context"`    // 上下文名称
	IsDefault  bool   `json:"is_default"`
}

// K8sNamespace 命名空间
type K8sNamespace struct {
	Name   string            `json:"name"`
	Status string            `json:"status"`
	Age    string            `json:"age"`
	Labels map[string]string `json:"labels,omitempty"`
}

// K8sDeployment 部署信息
type K8sDeployment struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Ready     string `json:"ready"`
	UpToDate  int    `json:"up_to_date"`
	Available int    `json:"available"`
	Age       string `json:"age"`
	Images    string `json:"images"`
	Replicas  int    `json:"replicas"`
	Strategy  string `json:"strategy,omitempty"`
}

// K8sService 服务信息
type K8sService struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Type       string `json:"type"`
	ClusterIP  string `json:"cluster_ip"`
	ExternalIP string `json:"external_ip"`
	Ports      string `json:"ports"`
	Age        string `json:"age"`
}

// K8sPod Pod信息
type K8sPod struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Ready     string `json:"ready"`
	Status    string `json:"status"`
	Restarts  int    `json:"restarts"`
	Age       string `json:"age"`
	IP        string `json:"ip"`
	Node      string `json:"node"`
}

// K8sNode 节点信息
type K8sNode struct {
	Name             string `json:"name"`
	Status           string `json:"status"`
	Roles            string `json:"roles"`
	Age              string `json:"age"`
	Version          string `json:"version"`
	InternalIP       string `json:"internal_ip"`
	OSImage          string `json:"os_image"`
	KernelVersion    string `json:"kernel_version"`
	ContainerRuntime string `json:"container_runtime"`
}

// K8sApplyRequest kubectl apply 请求
type K8sApplyRequest struct {
	Namespace   string `json:"namespace"`
	YamlPath    string `json:"yaml_path"`    // YAML 文件路径
	YamlContent string `json:"yaml_content"` // 或直接传 YAML 内容
}

// K8sApplyResult apply 结果
type K8sApplyResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Output  string `json:"output"`
}

// K8sRestartRequest 重启请求
type K8sRestartRequest struct {
	Namespace  string `json:"namespace"`
	Deployment string `json:"deployment"`
}

// K8sScaleRequest 扩缩容请求
type K8sScaleRequest struct {
	Namespace  string `json:"namespace"`
	Deployment string `json:"deployment"`
	Replicas   int    `json:"replicas"`
}

// K8sImageUpdateRequest 更新镜像请求
type K8sImageUpdateRequest struct {
	Namespace  string `json:"namespace"`
	Deployment string `json:"deployment"`
	Container  string `json:"container"`
	Image      string `json:"image"`
}

// execKubectl 执行 kubectl 命令
func execKubectl(args ...string) (string, error) {
	cmd := exec.Command("kubectl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%s: %s", err.Error(), stderr.String())
	}
	return stdout.String(), nil
}

// HandleGetNamespaces 获取命名空间列表
func HandleGetNamespaces(w http.ResponseWriter, r *http.Request) {
	output, err := execKubectl("get", "namespaces", "-o", "json")
	if err != nil {
		sendError(w, "获取命名空间失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var result struct {
		Items []struct {
			Metadata struct {
				Name              string            `json:"name"`
				CreationTimestamp string            `json:"creationTimestamp"`
				Labels            map[string]string `json:"labels"`
			} `json:"metadata"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}

	if err := json.Unmarshal([]byte(output), &result); err != nil {
		sendError(w, "解析命名空间数据失败", http.StatusInternalServerError)
		return
	}

	namespaces := make([]K8sNamespace, 0, len(result.Items))
	for _, item := range result.Items {
		age := calculateAge(item.Metadata.CreationTimestamp)
		namespaces = append(namespaces, K8sNamespace{
			Name:   item.Metadata.Name,
			Status: item.Status.Phase,
			Age:    age,
			Labels: item.Metadata.Labels,
		})
	}

	respondJSON(w, http.StatusOK, namespaces)
}

// HandleGetDeployments 获取部署列表
func HandleGetDeployments(w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}

	args := []string{"get", "deployments", "-n", namespace, "-o", "json"}
	if namespace == "all" {
		args = []string{"get", "deployments", "--all-namespaces", "-o", "json"}
	}

	output, err := execKubectl(args...)
	if err != nil {
		sendError(w, "获取部署列表失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var result struct {
		Items []struct {
			Metadata struct {
				Name              string `json:"name"`
				Namespace         string `json:"namespace"`
				CreationTimestamp string `json:"creationTimestamp"`
			} `json:"metadata"`
			Spec struct {
				Replicas int `json:"replicas"`
				Strategy struct {
					Type string `json:"type"`
				} `json:"strategy"`
				Template struct {
					Spec struct {
						Containers []struct {
							Name  string `json:"name"`
							Image string `json:"image"`
						} `json:"containers"`
					} `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
			Status struct {
				Replicas          int `json:"replicas"`
				ReadyReplicas     int `json:"readyReplicas"`
				UpdatedReplicas   int `json:"updatedReplicas"`
				AvailableReplicas int `json:"availableReplicas"`
			} `json:"status"`
		} `json:"items"`
	}

	if err := json.Unmarshal([]byte(output), &result); err != nil {
		sendError(w, "解析部署数据失败", http.StatusInternalServerError)
		return
	}

	deployments := make([]K8sDeployment, 0, len(result.Items))
	for _, item := range result.Items {
		images := make([]string, 0)
		for _, c := range item.Spec.Template.Spec.Containers {
			images = append(images, c.Image)
		}

		age := calculateAge(item.Metadata.CreationTimestamp)
		deployments = append(deployments, K8sDeployment{
			Name:      item.Metadata.Name,
			Namespace: item.Metadata.Namespace,
			Ready:     fmt.Sprintf("%d/%d", item.Status.ReadyReplicas, item.Spec.Replicas),
			UpToDate:  item.Status.UpdatedReplicas,
			Available: item.Status.AvailableReplicas,
			Age:       age,
			Images:    strings.Join(images, ", "),
			Replicas:  item.Spec.Replicas,
			Strategy:  item.Spec.Strategy.Type,
		})
	}

	respondJSON(w, http.StatusOK, deployments)
}

// HandleGetServices 获取服务列表
func HandleGetServices(w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}

	args := []string{"get", "services", "-n", namespace, "-o", "json"}
	if namespace == "all" {
		args = []string{"get", "services", "--all-namespaces", "-o", "json"}
	}

	output, err := execKubectl(args...)
	if err != nil {
		sendError(w, "获取服务列表失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var result struct {
		Items []struct {
			Metadata struct {
				Name              string `json:"name"`
				Namespace         string `json:"namespace"`
				CreationTimestamp string `json:"creationTimestamp"`
			} `json:"metadata"`
			Spec struct {
				Type       string   `json:"type"`
				ClusterIP  string   `json:"clusterIP"`
				ExternalIP []string `json:"externalIPs"`
				Ports      []struct {
					Port       int         `json:"port"`
					TargetPort interface{} `json:"targetPort"`
					NodePort   int         `json:"nodePort,omitempty"`
					Protocol   string      `json:"protocol"`
				} `json:"ports"`
			} `json:"spec"`
		} `json:"items"`
	}

	if err := json.Unmarshal([]byte(output), &result); err != nil {
		sendError(w, "解析服务数据失败", http.StatusInternalServerError)
		return
	}

	services := make([]K8sService, 0, len(result.Items))
	for _, item := range result.Items {
		ports := make([]string, 0)
		for _, p := range item.Spec.Ports {
			if p.NodePort > 0 {
				ports = append(ports, fmt.Sprintf("%d:%v/%s", p.Port, p.TargetPort, p.Protocol))
			} else {
				ports = append(ports, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
			}
		}

		externalIP := "<none>"
		if len(item.Spec.ExternalIP) > 0 {
			externalIP = strings.Join(item.Spec.ExternalIP, ",")
		}

		age := calculateAge(item.Metadata.CreationTimestamp)
		services = append(services, K8sService{
			Name:       item.Metadata.Name,
			Namespace:  item.Metadata.Namespace,
			Type:       item.Spec.Type,
			ClusterIP:  item.Spec.ClusterIP,
			ExternalIP: externalIP,
			Ports:      strings.Join(ports, ", "),
			Age:        age,
		})
	}

	respondJSON(w, http.StatusOK, services)
}

// HandleGetPods 获取 Pod 列表
func HandleGetPods(w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}

	args := []string{"get", "pods", "-n", namespace, "-o", "json"}
	if namespace == "all" {
		args = []string{"get", "pods", "--all-namespaces", "-o", "json"}
	}

	output, err := execKubectl(args...)
	if err != nil {
		sendError(w, "获取 Pod 列表失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var result struct {
		Items []struct {
			Metadata struct {
				Name              string `json:"name"`
				Namespace         string `json:"namespace"`
				CreationTimestamp string `json:"creationTimestamp"`
			} `json:"metadata"`
			Spec struct {
				NodeName string `json:"nodeName"`
			} `json:"spec"`
			Status struct {
				Phase             string `json:"phase"`
				PodIP             string `json:"podIP"`
				ContainerStatuses []struct {
					Ready        bool `json:"ready"`
					RestartCount int  `json:"restartCount"`
				} `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}

	if err := json.Unmarshal([]byte(output), &result); err != nil {
		sendError(w, "解析 Pod 数据失败", http.StatusInternalServerError)
		return
	}

	pods := make([]K8sPod, 0, len(result.Items))
	for _, item := range result.Items {
		readyCount := 0
		totalCount := len(item.Status.ContainerStatuses)
		restarts := 0
		for _, cs := range item.Status.ContainerStatuses {
			if cs.Ready {
				readyCount++
			}
			restarts += cs.RestartCount
		}

		age := calculateAge(item.Metadata.CreationTimestamp)
		pods = append(pods, K8sPod{
			Name:      item.Metadata.Name,
			Namespace: item.Metadata.Namespace,
			Ready:     fmt.Sprintf("%d/%d", readyCount, totalCount),
			Status:    item.Status.Phase,
			Restarts:  restarts,
			Age:       age,
			IP:        item.Status.PodIP,
			Node:      item.Spec.NodeName,
		})
	}

	respondJSON(w, http.StatusOK, pods)
}

// HandleGetNodes 获取节点列表
func HandleGetNodes(w http.ResponseWriter, r *http.Request) {
	output, err := execKubectl("get", "nodes", "-o", "json")
	if err != nil {
		sendError(w, "获取节点列表失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var result struct {
		Items []struct {
			Metadata struct {
				Name              string            `json:"name"`
				Labels            map[string]string `json:"labels"`
				CreationTimestamp string            `json:"creationTimestamp"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
				NodeInfo struct {
					KubeletVersion          string `json:"kubeletVersion"`
					OSImage                 string `json:"osImage"`
					KernelVersion           string `json:"kernelVersion"`
					ContainerRuntimeVersion string `json:"containerRuntimeVersion"`
				} `json:"nodeInfo"`
				Addresses []struct {
					Type    string `json:"type"`
					Address string `json:"address"`
				} `json:"addresses"`
			} `json:"status"`
		} `json:"items"`
	}

	if err := json.Unmarshal([]byte(output), &result); err != nil {
		sendError(w, "解析节点数据失败", http.StatusInternalServerError)
		return
	}

	nodes := make([]K8sNode, 0, len(result.Items))
	for _, item := range result.Items {
		// 获取节点状态
		status := "Unknown"
		for _, cond := range item.Status.Conditions {
			if cond.Type == "Ready" {
				if cond.Status == "True" {
					status = "Ready"
				} else {
					status = "NotReady"
				}
				break
			}
		}

		// 获取节点角色
		roles := make([]string, 0)
		for label := range item.Metadata.Labels {
			if strings.HasPrefix(label, "node-role.kubernetes.io/") {
				role := strings.TrimPrefix(label, "node-role.kubernetes.io/")
				roles = append(roles, role)
			}
		}
		if len(roles) == 0 {
			roles = append(roles, "<none>")
		}

		// 获取内部 IP
		internalIP := ""
		for _, addr := range item.Status.Addresses {
			if addr.Type == "InternalIP" {
				internalIP = addr.Address
				break
			}
		}

		age := calculateAge(item.Metadata.CreationTimestamp)
		nodes = append(nodes, K8sNode{
			Name:             item.Metadata.Name,
			Status:           status,
			Roles:            strings.Join(roles, ","),
			Age:              age,
			Version:          item.Status.NodeInfo.KubeletVersion,
			InternalIP:       internalIP,
			OSImage:          item.Status.NodeInfo.OSImage,
			KernelVersion:    item.Status.NodeInfo.KernelVersion,
			ContainerRuntime: item.Status.NodeInfo.ContainerRuntimeVersion,
		})
	}

	respondJSON(w, http.StatusOK, nodes)
}

// HandleK8sApply 执行 kubectl apply
func HandleK8sApply(w http.ResponseWriter, r *http.Request) {
	var req K8sApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	var output string
	var err error

	if req.YamlPath != "" {
		// 从文件 apply
		args := []string{"apply", "-f", req.YamlPath}
		if req.Namespace != "" {
			args = append(args, "-n", req.Namespace)
		}
		output, err = execKubectl(args...)
	} else if req.YamlContent != "" {
		// 从内容 apply（使用 stdin）
		cmd := exec.Command("kubectl", "apply", "-f", "-")
		if req.Namespace != "" {
			cmd = exec.Command("kubectl", "apply", "-f", "-", "-n", req.Namespace)
		}
		cmd.Stdin = strings.NewReader(req.YamlContent)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err = cmd.Run()
		output = stdout.String()
		if err != nil {
			output += stderr.String()
		}
	} else {
		sendError(w, "请提供 yaml_path 或 yaml_content", http.StatusBadRequest)
		return
	}

	if err != nil {
		respondJSON(w, http.StatusOK, K8sApplyResult{
			Success: false,
			Message: "Apply 失败",
			Output:  output,
		})
		return
	}

	respondJSON(w, http.StatusOK, K8sApplyResult{
		Success: true,
		Message: "Apply 成功",
		Output:  output,
	})
}

// HandleK8sRestartDeployment 重启部署（滚动更新）
func HandleK8sRestartDeployment(w http.ResponseWriter, r *http.Request) {
	var req K8sRestartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	if req.Namespace == "" || req.Deployment == "" {
		sendError(w, "请提供 namespace 和 deployment", http.StatusBadRequest)
		return
	}

	output, err := execKubectl("rollout", "restart", "deployment", req.Deployment, "-n", req.Namespace)
	if err != nil {
		respondJSON(w, http.StatusOK, K8sApplyResult{
			Success: false,
			Message: "重启失败",
			Output:  err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, K8sApplyResult{
		Success: true,
		Message: "重启成功",
		Output:  output,
	})
}

// HandleK8sScaleDeployment 扩缩容
func HandleK8sScaleDeployment(w http.ResponseWriter, r *http.Request) {
	var req K8sScaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	if req.Namespace == "" || req.Deployment == "" {
		sendError(w, "请提供 namespace 和 deployment", http.StatusBadRequest)
		return
	}

	output, err := execKubectl("scale", "deployment", req.Deployment,
		"--replicas", fmt.Sprintf("%d", req.Replicas), "-n", req.Namespace)
	if err != nil {
		respondJSON(w, http.StatusOK, K8sApplyResult{
			Success: false,
			Message: "扩缩容失败",
			Output:  err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, K8sApplyResult{
		Success: true,
		Message: fmt.Sprintf("已调整副本数为 %d", req.Replicas),
		Output:  output,
	})
}

// HandleK8sUpdateImage 更新镜像
func HandleK8sUpdateImage(w http.ResponseWriter, r *http.Request) {
	var req K8sImageUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	if req.Namespace == "" || req.Deployment == "" || req.Container == "" || req.Image == "" {
		sendError(w, "请提供完整参数", http.StatusBadRequest)
		return
	}

	output, err := execKubectl("set", "image", "deployment/"+req.Deployment,
		req.Container+"="+req.Image, "-n", req.Namespace)
	if err != nil {
		respondJSON(w, http.StatusOK, K8sApplyResult{
			Success: false,
			Message: "更新镜像失败",
			Output:  err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, K8sApplyResult{
		Success: true,
		Message: "镜像更新成功",
		Output:  output,
	})
}

// HandleK8sGetDeploymentYaml 获取部署的 YAML
func HandleK8sGetDeploymentYaml(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}

	output, err := execKubectl("get", "deployment", name, "-n", namespace, "-o", "yaml")
	if err != nil {
		sendError(w, "获取 YAML 失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(output))
}

// HandleK8sGetPodLogs 获取 Pod 日志
func HandleK8sGetPodLogs(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]
	namespace := r.URL.Query().Get("namespace")
	container := r.URL.Query().Get("container")
	tail := r.URL.Query().Get("tail")

	if namespace == "" {
		namespace = "default"
	}
	if tail == "" {
		tail = "100"
	}

	args := []string{"logs", name, "-n", namespace, "--tail", tail}
	if container != "" {
		args = append(args, "-c", container)
	}

	output, err := execKubectl(args...)
	if err != nil {
		sendError(w, "获取日志失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(output))
}

// HandleK8sDeletePod 删除 Pod
func HandleK8sDeletePod(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]
	namespace := r.URL.Query().Get("namespace")

	if namespace == "" {
		namespace = "default"
	}

	output, err := execKubectl("delete", "pod", name, "-n", namespace)
	if err != nil {
		respondJSON(w, http.StatusOK, K8sApplyResult{
			Success: false,
			Message: "删除 Pod 失败",
			Output:  err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, K8sApplyResult{
		Success: true,
		Message: "Pod 已删除",
		Output:  output,
	})
}

// HandleK8sRolloutStatus 获取滚动更新状态
func HandleK8sRolloutStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]
	namespace := r.URL.Query().Get("namespace")

	if namespace == "" {
		namespace = "default"
	}

	output, err := execKubectl("rollout", "status", "deployment", name, "-n", namespace, "--timeout=5s")
	if err != nil {
		// 超时不一定是错误，可能还在进行中
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "in_progress",
			"message": output,
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "completed",
		"message": output,
	})
}

// HandleK8sRolloutHistory 获取滚动更新历史
func HandleK8sRolloutHistory(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]
	namespace := r.URL.Query().Get("namespace")

	if namespace == "" {
		namespace = "default"
	}

	output, err := execKubectl("rollout", "history", "deployment", name, "-n", namespace)
	if err != nil {
		sendError(w, "获取历史失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(output))
}

// HandleK8sRollback 回滚部署
func HandleK8sRollback(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]

	var req struct {
		Namespace string `json:"namespace"`
		Revision  int    `json:"revision"` // 可选，不指定则回滚到上一版本
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "请求参数无效", http.StatusBadRequest)
		return
	}

	if req.Namespace == "" {
		req.Namespace = "default"
	}

	args := []string{"rollout", "undo", "deployment", name, "-n", req.Namespace}
	if req.Revision > 0 {
		args = append(args, "--to-revision", fmt.Sprintf("%d", req.Revision))
	}

	output, err := execKubectl(args...)
	if err != nil {
		respondJSON(w, http.StatusOK, K8sApplyResult{
			Success: false,
			Message: "回滚失败",
			Output:  err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, K8sApplyResult{
		Success: true,
		Message: "回滚成功",
		Output:  output,
	})
}

// calculateAge 计算资源年龄
func calculateAge(creationTimestamp string) string {
	t, err := time.Parse(time.RFC3339, creationTimestamp)
	if err != nil {
		return "Unknown"
	}

	duration := time.Since(t)

	if duration.Hours() >= 24*365 {
		return fmt.Sprintf("%dy", int(duration.Hours()/(24*365)))
	}
	if duration.Hours() >= 24*30 {
		return fmt.Sprintf("%dmo", int(duration.Hours()/(24*30)))
	}
	if duration.Hours() >= 24 {
		return fmt.Sprintf("%dd", int(duration.Hours()/24))
	}
	if duration.Hours() >= 1 {
		return fmt.Sprintf("%dh", int(duration.Hours()))
	}
	if duration.Minutes() >= 1 {
		return fmt.Sprintf("%dm", int(duration.Minutes()))
	}
	return fmt.Sprintf("%ds", int(duration.Seconds()))
}
