package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"monitoring-tool/backend/internal/chat"
)

type chatAsker interface {
	Ask(ctx context.Context, message string) (chat.Response, error)
}

type ChatHandler struct {
	service chatAsker
}

type chatRequest struct {
	Message string `json:"message"`
}

func NewChatHandler(service chatAsker) *ChatHandler {
	return &ChatHandler{service: service}
}

func (h *ChatHandler) Query(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("message is required"))
		return
	}

	response, err := h.service.Ask(r.Context(), req.Message)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}
