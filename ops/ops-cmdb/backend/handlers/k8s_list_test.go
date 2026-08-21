package handlers

import "testing"

// ★ COUNT(*) 必须复用**最外层**的 FROM。
//
// 选择列表里的子查询也带 FROM，取第一个的话 COUNT 语句会被拼成语法错误，
// 分页请求直接 500。这个 bug 在 Nodes 接口上长期存在 ——
// 不带 page 参数走的是另一条分支，所以没人发现。
func TestTopLevelFrom(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			"简单查询",
			"SELECT id,name FROM k8s_pods",
			" FROM k8s_pods",
		},
		{
			"选择列表含子查询（真实案例：k8s_nodes）",
			"SELECT id,name,COALESCE((SELECT c.id FROM cis c WHERE c.name=k8s_nodes.name LIMIT 1),0) AS host_ci_id FROM k8s_nodes",
			" FROM k8s_nodes",
		},
		{
			"含 CASE 表达式",
			"SELECT id,CASE WHEN stuck=1 THEN '异常' ELSE '正常' END AS health FROM k8s_nodes",
			" FROM k8s_nodes",
		},
		{
			"嵌套两层子查询",
			"SELECT (SELECT x FROM (SELECT y FROM z) t) AS v FROM main_table",
			" FROM main_table",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := topLevelFrom(c.in); got != c.want {
				t.Errorf("topLevelFrom() = %q, want %q", got, c.want)
			}
		})
	}
}
