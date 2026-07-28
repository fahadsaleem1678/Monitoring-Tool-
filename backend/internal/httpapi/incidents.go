package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"monitoring-tool/backend/internal/auth"
	"monitoring-tool/backend/internal/notify"
	"monitoring-tool/backend/internal/store"
)

type IncidentHandler struct {
	store      *store.Store
	slack      *notify.SlackNotifier
	agentToken string
}

func NewIncidentHandler(store *store.Store, slack *notify.SlackNotifier, agentToken string) *IncidentHandler {
	return &IncidentHandler{store: store, slack: slack, agentToken: strings.TrimSpace(agentToken)}
}

type investigationRequest struct {
	Summary      string                            `json:"summary"`
	Confidence   string                            `json:"confidence"`
	DraftMessage string                            `json:"draft_message"`
	Steps        []store.IncidentInvestigationStep `json:"steps"`
}

type draftRequest struct {
	DraftMessage string `json:"draft_message"`
}

type approveRequest struct {
	FinalMessage string `json:"final_message"`
}

func (h *IncidentHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, ok := limitFromRequest(w, r)
	if !ok {
		return
	}
	reviews, err := h.store.ListIncidentReviews(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incidents": reviews})
}

func (h *IncidentHandler) ByID(w http.ResponseWriter, r *http.Request) {
	id, ok := incidentIDFromPath(w, r)
	if !ok {
		return
	}
	review, err := h.store.IncidentReviewByID(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incident": review})
}

func (h *IncidentHandler) UpdateDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := incidentIDFromPath(w, r)
	if !ok {
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	var req draftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	draft := strings.TrimSpace(req.DraftMessage)
	if draft == "" {
		writeError(w, http.StatusBadRequest, errors.New("draft_message is required"))
		return
	}
	review, err := h.store.UpdateIncidentDraft(r.Context(), id, draft, user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incident": review})
}

func (h *IncidentHandler) Approve(w http.ResponseWriter, r *http.Request) {
	id, ok := incidentIDFromPath(w, r)
	if !ok {
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	var req approveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	message := strings.TrimSpace(req.FinalMessage)
	if message == "" {
		writeError(w, http.StatusBadRequest, errors.New("final_message is required"))
		return
	}
	review, err := h.store.ApproveIncidentReview(r.Context(), id, message, user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incident": review})
}

func (h *IncidentHandler) Reject(w http.ResponseWriter, r *http.Request) {
	id, ok := incidentIDFromPath(w, r)
	if !ok {
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	review, err := h.store.RejectIncidentReview(r.Context(), id, user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incident": review})
}

func (h *IncidentHandler) Broadcast(w http.ResponseWriter, r *http.Request) {
	id, ok := incidentIDFromPath(w, r)
	if !ok {
		return
	}
	review, err := h.store.IncidentReviewByID(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if review.Status != "approved" {
		writeError(w, http.StatusBadRequest, errors.New("incident must be approved before broadcast"))
		return
	}
	if err := h.slack.Send(r.Context(), review.FinalMessage); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	review, err = h.store.MarkIncidentBroadcasted(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	_ = h.store.CreateIncidentAuditEvent(r.Context(), id, "system", nil, "broadcasted", map[string]any{"channel": "slack"})
	writeJSON(w, http.StatusOK, map[string]any{"incident": review})
}

func (h *IncidentHandler) AgentList(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAgent(w, r) {
		return
	}
	limit, ok := limitFromRequest(w, r)
	if !ok {
		return
	}
	reviews, err := h.store.ListIncidentReviews(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incidents": reviews})
}

func (h *IncidentHandler) AgentClaim(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAgent(w, r) {
		return
	}
	id, ok := incidentIDFromPath(w, r)
	if !ok {
		return
	}
	review, err := h.store.ClaimIncidentReview(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	_ = h.store.CreateIncidentAuditEvent(r.Context(), id, "agent", nil, "claimed", map[string]any{})
	writeJSON(w, http.StatusOK, map[string]any{"incident": review})
}

func (h *IncidentHandler) AgentComplete(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAgent(w, r) {
		return
	}
	id, ok := incidentIDFromPath(w, r)
	if !ok {
		return
	}
	var req investigationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Summary) == "" || strings.TrimSpace(req.DraftMessage) == "" {
		writeError(w, http.StatusBadRequest, errors.New("summary and draft_message are required"))
		return
	}
	review, err := h.store.CompleteIncidentInvestigation(r.Context(), id, req.Summary, req.Confidence, req.DraftMessage, req.Steps)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	_ = h.store.CreateIncidentAuditEvent(r.Context(), id, "agent", nil, "investigation_completed", map[string]any{"steps": len(req.Steps)})
	writeJSON(w, http.StatusOK, map[string]any{"incident": review})
}

func (h *IncidentHandler) authorizeAgent(w http.ResponseWriter, r *http.Request) bool {
	token := strings.TrimSpace(r.Header.Get("X-Agent-Token"))
	if token == "" || token != h.agentToken {
		writeError(w, http.StatusUnauthorized, errors.New("invalid agent token"))
		return false
	}
	return true
}

func limitFromRequest(w http.ResponseWriter, r *http.Request) (int, bool) {
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, errors.New("limit must be a number"))
			return 0, false
		}
		limit = parsed
	}
	return limit, true
}

func incidentIDFromPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid incident id"))
		return uuid.Nil, false
	}
	return id, true
}
