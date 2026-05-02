package tasks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// AnsibleRunner 把 Spec 翻译成实际要跑的 shell 命令
//
//	v5 起 git 同步从 work 命令里剥离，改由 manager 在 batch 入口统一跑一次（gitMu 保护）：
//	  rsync work:           cd <ansible_root> && sudo python <proj>/<proj>.py <service>
//	  update_version work:  cd <ansible_root> && sudo ansible-playbook -i ... <service>.yaml -e deploy_version=...
//	  git sync (manager):   cd <ansible_root> && git checkout main && git reset --hard && git pull
//
//	不再每个 task 自己 git pull → N 个 service 并行不会撞 .git/index.lock；
//	git pull 失败 → 整批 failed（语义跟 K8s update_image 对齐）
type AnsibleRunner struct {
	AnsibleRoot string // /etc/ansible
}

// BuildCommand 构造仅执行「业务工作」的命令（不含 git 同步）。
// ActionGitSync 不该走这里 —— manager 直接调 BuildGitSyncCommand。
func (r *AnsibleRunner) BuildCommand(s Spec) (*exec.Cmd, error) {
	switch s.Action {
	case ActionRsync:
		return r.buildRsync(s)
	case ActionUpdateVersion:
		return r.buildUpdateVersion(s)
	case ActionGitSync:
		// git_sync 没有「work」部分；manager 应特判走 GitSync 路径而不是这里
		return nil, fmt.Errorf("ActionGitSync 不该走 BuildCommand；manager 应直接调 GitSync()")
	default:
		return nil, fmt.Errorf("unknown action: %s", s.Action)
	}
}

// BuildGitSyncCommand 单独的 git 同步命令；manager 在 gitMu 保护下调用，给整批 task 做前置。
//
//	checkout main + reset --hard + pull，跟 V1 行为完全一致；
//	失败由 manager 解析 stderr 决定是否重试（白名单瞬时错误重试 3 次）
func (r *AnsibleRunner) BuildGitSyncCommand() *exec.Cmd {
	script := fmt.Sprintf(`set -e
cd %s
git checkout main
git reset --hard
git pull`, shQuote(r.AnsibleRoot))
	return exec.Command("/bin/bash", "-c", script)
}

func (r *AnsibleRunner) buildRsync(s Spec) (*exec.Cmd, error) {
	pyPath := filepath.Join(r.AnsibleRoot, s.Project, s.Project+".py")
	// rsync 走 python 脚本，跟 ansible 没关系，sudo 不用透传 ANSIBLE_*
	script := fmt.Sprintf(`set -e
cd %s
sudo python %s %s`,
		shQuote(r.AnsibleRoot),
		shQuote(pyPath),
		shQuote(s.Service))
	return exec.Command("/bin/bash", "-c", script), nil
}

func (r *AnsibleRunner) buildUpdateVersion(s Spec) (*exec.Cmd, error) {
	inventory := filepath.Join(r.AnsibleRoot, "inventory", s.Project, s.Env,
		fmt.Sprintf("%s_%s_hosts", s.Project, s.Env))
	playbook := filepath.Join(r.AnsibleRoot, s.Project, s.Env, s.Service+".yaml")

	// agent 进程自己有 ANSIBLE_LOCAL_TEMP / ANSIBLE_REMOTE_TEMP（systemd Environment 注入），
	// 但 sudo 默认会剥光 env，所以要在 sudo 命令行里显式重申，写成 `sudo VAR=value cmd` 形式。
	// （这要 sudoers 没 `Defaults !setenv`，绝大多数发行版默认允许）
	envPrefix := buildAnsibleEnvPrefix()

	script := fmt.Sprintf(`set -e
cd %s
sudo %sansible-playbook -i %s %s -e deploy_version=%s --diff`,
		shQuote(r.AnsibleRoot),
		envPrefix,
		shQuote(inventory),
		shQuote(playbook),
		shQuote(s.Version))
	return exec.Command("/bin/bash", "-c", script), nil
}

// buildAnsibleEnvPrefix 把 agent 进程的所有 ANSIBLE_* 环境变量透传给 sudo。
//
//	返回形如 `ANSIBLE_LOCAL_TEMP='/data/xxx' ANSIBLE_SSH_CONTROL_PATH_DIR='/data/xxx/cp' `
//	（末尾带空格），让调用方拼到 `sudo <prefix>ansible-playbook ...` 中间。
//	没有任何 ANSIBLE_* env 时返空串。
//
//	设计：白名单按"前缀"而不是"具体变量名"——
//	  - sudo 默认会清环境变量，必须显式 `sudo VAR=value cmd` 透传
//	  - ansible 子进程要写 ~/.ansible/{tmp,cp,galaxy_cache,...} 等多个目录
//	    被 systemd ProtectHome=read-only 挡住时，每种都得有对应的 ANSIBLE_* 重定向
//	  - 未来 ansible 加新用法（写新目录）时，只要 systemd Environment 加一行新的 ANSIBLE_X，
//	    本函数自动透传，**不用改 agent 代码 / 重出镜像**
//
//	安全性：前缀白名单 ANSIBLE_* 比"全开"更收敛。攻击面只覆盖 ansible 自己识别的 env，
//	并且这些 env 必须先在 systemd Environment 显式声明（用户主动控制）才会出现在进程里。
//
//	常见可重定向到可写路径的 env 名（用户可在 systemd unit 配置）：
//	  ANSIBLE_LOCAL_TEMP             控制机本地临时目录（默认 ~/.ansible/tmp）
//	  ANSIBLE_SSH_CONTROL_PATH_DIR   SSH ControlPath 目录（默认 ~/.ansible/cp）
//	  ANSIBLE_GALAXY_CACHE_DIR       galaxy 缓存（默认 ~/.ansible/galaxy_cache）
//	  ANSIBLE_PERSISTENT_CACHE_DIR   持久化连接缓存（默认 ~/.ansible/persistent_cache）
//	  ANSIBLE_ASYNC_DIR              async 任务目录（默认 ~/.ansible_async）
func buildAnsibleEnvPrefix() string {
	parts := []string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "ANSIBLE_") {
			continue
		}
		i := strings.IndexByte(kv, '=')
		if i <= 0 { // 防御：没有 '=' 或 key 为空
			continue
		}
		k, v := kv[:i], kv[i+1:]
		if v == "" {
			continue
		}
		parts = append(parts, k+"="+shQuote(v))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ") + " "
}

// shQuote 把字符串包成单引号字符串供 bash 用，转义字符串内的单引号
//
//	bash 单引号内不解析任何转义；遇到内嵌的单引号用 '\''（结束当前引号 → 转义引号 → 重开引号）
func shQuote(s string) string {
	out := []byte{'\''}
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			out = append(out, '\'', '\\', '\'', '\'')
			continue
		}
		out = append(out, s[i])
	}
	out = append(out, '\'')
	return string(out)
}
