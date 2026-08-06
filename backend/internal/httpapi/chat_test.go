package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"monitoring-tool/backend/internal/chat"
)

type fakeChatService struct {
	response chat.Response
	err      error
	message  string
}

func (f *fakeChatService) Ask(_ context.Context, message string) (chat.Response, error) {
	f.message = message
	return f.response, f.err
}

func TestChatHandlerQueryReturnsAnswer(t *testing.T) {
	service := &fakeChatService{
		response: chat.Response{
			Answer:     "There are 2 pods showing CrashLoopBackOff in monitoring-tool.",
			Intent:     "pod_crashloops",
			Confidence: chat.ConfidenceHigh,
			Facts: []chat.Fact{
				{Label: "CrashLoopBackOff pods", Value: "2", Severity: "warning"},
			},
			Queries:     []string{`sum(kube_pod_container_status_waiting_reason{namespace="monitoring-tool",reason="CrashLoopBackOff"})`},
			Suggestions: []string{"Which pods are restarting?"},
		},
	}
	handler := NewChatHandler(service)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/query", strings.NewReader(`{"message":"any crash loops?"}`))
	rec := httptest.NewRecorder()

	handler.Query(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if service.message != "any crash loops?" {
		t.Fatalf("message = %q, want posted message", service.message)
	}

	var body chat.Response
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Intent != "pod_crashloops" {
		t.Fatalf("intent = %q, want pod_crashloops", body.Intent)
	}
}

func TestChatHandlerQueryRejectsEmptyMessage(t *testing.T) {
	handler := NewChatHandler(&fakeChatService{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/query", strings.NewReader(`{"message":" "}`))
	rec := httptest.NewRecorder()

	handler.Query(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestChatHandlerQueryRejectsInvalidJSON(t *testing.T) {
	handler := NewChatHandler(&fakeChatService{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/query", strings.NewReader(`{"message":`))
	rec := httptest.NewRecorder()

	handler.Query(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
