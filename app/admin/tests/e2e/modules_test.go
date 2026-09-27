package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func TestFileLifecycleAndOwnership(t *testing.T) {
	env := newTestEnv(t)
	env.grantRole(t, "file-owner", "system:file:add", "system:file:remove")
	token := env.kc.userToken(t, "file-owner", "file-owner")

	path := "/v1/system/files:upload?filename=" + url.QueryEscape("hello.txt")
	code, _, body := env.doRaw(t, http.MethodPost, path, token, "text/plain", []byte("hello eagle"))
	if code != http.StatusOK {
		t.Fatalf("upload = %d (%s)", code, body)
	}
	var uploaded struct {
		File struct {
			ID     string `json:"id"`
			SHA256 string `json:"sha256"`
			Size   int64  `json:"size"`
		} `json:"file"`
	}
	if err := json.Unmarshal(body, &uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.File.ID == "" || uploaded.File.Size != int64(len("hello eagle")) || uploaded.File.SHA256 == "" {
		t.Fatalf("unexpected upload response: %s", body)
	}

	code, header, content := env.doRaw(t, http.MethodGet,
		fmt.Sprintf("/v1/system/files/%s/content", uploaded.File.ID), token, "", nil)
	if code != http.StatusOK || string(content) != "hello eagle" {
		t.Fatalf("download = %d (%q)", code, content)
	}
	if header.Get("Content-Type") != "text/plain" || header.Get("Content-Disposition") != `attachment; filename=hello.txt` {
		t.Fatalf("download headers = %v", header)
	}

	other := env.kc.userToken(t, "other", "file-owner")
	if code, _ := env.get(t, "/v1/system/files/"+uploaded.File.ID, other); code != http.StatusNotFound {
		t.Fatalf("another subject reading file = %d, want 404", code)
	}

	if code, body := env.do(t, http.MethodDelete, "/v1/system/files/"+uploaded.File.ID, token, ""); code != http.StatusOK {
		t.Fatalf("delete = %d (%s)", code, body)
	}
}

func TestNotificationLifecycleIsRecipientScoped(t *testing.T) {
	env := newTestEnv(t)
	env.grantRole(t, "notifier", "system:notification:send")
	sender := env.kc.userToken(t, "sender", "notifier")
	recipient := env.kc.userToken(t, "recipient")

	code, body := env.do(t, http.MethodPost, "/v1/system/notifications", sender,
		`{"recipient_subject":"sub-recipient","title":"部署完成","content":"版本已发布"}`)
	if code != http.StatusOK {
		t.Fatalf("send = %d (%s)", code, body)
	}
	var sent struct {
		Notification struct {
			ID int64 `json:"id"`
		} `json:"notification"`
	}
	if err := json.Unmarshal([]byte(body), &sent); err != nil || sent.Notification.ID == 0 {
		t.Fatalf("decode notification: %v (%s)", err, body)
	}

	code, body = env.get(t, "/v1/system/notifications?unread_only=true", recipient)
	if code != http.StatusOK {
		t.Fatalf("list = %d (%s)", code, body)
	}
	var listed struct {
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal([]byte(body), &listed); err != nil || listed.Total < 1 {
		t.Fatalf("unexpected list: %v (%s)", err, body)
	}

	path := fmt.Sprintf("/v1/system/notifications/%d:read", sent.Notification.ID)
	if code, body := env.do(t, http.MethodPut, path, recipient, ""); code != http.StatusOK {
		t.Fatalf("mark read = %d (%s)", code, body)
	}
	code, body = env.get(t, "/v1/system/notifications/unread-count", recipient)
	var count struct {
		Count int64 `json:"count,string"`
	}
	if err := json.Unmarshal([]byte(body), &count); code != http.StatusOK || err != nil || count.Count != 0 {
		t.Fatalf("unread count = %d, err=%v (%s)", code, err, body)
	}
}
