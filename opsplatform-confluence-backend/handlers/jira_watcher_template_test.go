package handlers

import (
	"strings"
	"testing"
	"time"

	"opsplatform-confluence-backend/models"
)

func TestRenderNotifyTemplate_Default(t *testing.T) {
	vars := TemplateVars{
		IssueKey:   "YX-1023",
		Summary:    "用户中心上线",
		FromStatus: "研发HOD审批",
		ToStatus:   "运维组长审批",
		Assignee:   "corwin",
		Reporter:   "cesar",
		Priority:   "High",
		UpdatedAt:  "2026-05-13 14:30:00",
	}
	out, err := RenderNotifyTemplate("", vars)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if !strings.Contains(out, "YX-1023") || !strings.Contains(out, "运维组长审批") {
		t.Fatalf("missing expected content: %s", out)
	}
}

func TestRenderNotifyTemplate_Custom(t *testing.T) {
	vars := TemplateVars{IssueKey: "OPS-5", Summary: "x"}
	out, err := RenderNotifyTemplate("[{{.IssueKey}}] {{.Summary}}", vars)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if out != "[OPS-5] x" {
		t.Fatalf("got %q", out)
	}
}

func TestRenderNotifyTemplate_InvalidSyntax(t *testing.T) {
	_, err := RenderNotifyTemplate("{{.Missing", TemplateVars{})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestBuildInteractiveCard_WithAtUsers(t *testing.T) {
	task := models.SendTask{
		IssueKey:     "YX-1023",
		ToStatus:     "运维组长审批",
		TransitionAt: time.Now(),
		JiraIssueURL: "http://jira/browse/YX-1023",
	}
	users := []models.AtUser{{OpenID: "ou_abc", Name: "王五"}}
	card := BuildInteractiveCard(task, users, "正文")
	elements := card["elements"].([]map[string]interface{})
	// 应包含 div 正文 + div @ 段 + action 按钮 = 3 块
	if len(elements) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(elements))
	}
}

func TestBuildInteractiveCard_NoAtUsers(t *testing.T) {
	task := models.SendTask{IssueKey: "YX-1", ToStatus: "X", JiraIssueURL: "http://j"}
	card := BuildInteractiveCard(task, nil, "正文")
	elements := card["elements"].([]map[string]interface{})
	// 应只有 div 正文 + action 按钮 = 2 块
	if len(elements) != 2 {
		t.Fatalf("expected 2 elements (no at), got %d", len(elements))
	}
}
