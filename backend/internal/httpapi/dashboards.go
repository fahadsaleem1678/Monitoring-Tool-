package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"monitoring-tool/backend/internal/auth"
	promclient "monitoring-tool/backend/internal/prometheus"
	"monitoring-tool/backend/internal/store"
)

var (
	errInvalidCredentials = errors.New("invalid username or password")
	errUnauthorized       = errors.New("unauthorized")
)

type DashboardHandler struct {
	store      *store.Store
	prometheus *promclient.Client
}

func NewDashboardHandler(store *store.Store, prometheus *promclient.Client) *DashboardHandler {
	return &DashboardHandler{store: store, prometheus: prometheus}
}

type dashboardRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type panelRequest struct {
	Title                  string         `json:"title"`
	PromQL                 string         `json:"promql"`
	VisualizationType      string         `json:"visualization_type"`
	GridX                  int            `json:"grid_x"`
	GridY                  int            `json:"grid_y"`
	GridW                  int            `json:"grid_w"`
	GridH                  int            `json:"grid_h"`
	RefreshIntervalSeconds int            `json:"refresh_interval_seconds"`
	SettingsJSON           map[string]any `json:"settings_json"`
}

type previewRequest struct {
	PromQL string `json:"promql"`
}

func (h *DashboardHandler) List(w http.ResponseWriter, r *http.Request) {
	dashboards, err := h.store.ListDashboards(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dashboards": dashboards})
}

func (h *DashboardHandler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, errUnauthorized)
		return
	}

	req, ok := decodeDashboardRequest(w, r)
	if !ok {
		return
	}
	dashboard, err := h.store.CreateDashboard(r.Context(), req.Title, req.Description, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"dashboard": dashboard})
}

func (h *DashboardHandler) ByID(w http.ResponseWriter, r *http.Request) {
	id, ok := dashboardIDFromPath(w, r)
	if !ok {
		return
	}
	dashboard, err := h.store.DashboardByID(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dashboard": dashboard})
}

func (h *DashboardHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := dashboardIDFromPath(w, r)
	if !ok {
		return
	}
	req, ok := decodeDashboardRequest(w, r)
	if !ok {
		return
	}
	dashboard, err := h.store.UpdateDashboard(r.Context(), id, req.Title, req.Description)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dashboard": dashboard})
}

func (h *DashboardHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := dashboardIDFromPath(w, r)
	if !ok {
		return
	}
	if err := h.store.DeleteDashboard(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *DashboardHandler) CreatePanel(w http.ResponseWriter, r *http.Request) {
	dashboardID, ok := dashboardIDFromPath(w, r)
	if !ok {
		return
	}
	req, ok := decodePanelRequest(w, r)
	if !ok {
		return
	}
	panel := panelFromRequest(req)
	panel.DashboardID = dashboardID
	created, err := h.store.CreatePanel(r.Context(), panel)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"panel": created})
}

func (h *DashboardHandler) UpdatePanel(w http.ResponseWriter, r *http.Request) {
	panelID, ok := panelIDFromPath(w, r)
	if !ok {
		return
	}
	req, ok := decodePanelRequest(w, r)
	if !ok {
		return
	}
	panel := panelFromRequest(req)
	panel.ID = panelID
	updated, err := h.store.UpdatePanel(r.Context(), panel)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"panel": updated})
}

func (h *DashboardHandler) DeletePanel(w http.ResponseWriter, r *http.Request) {
	panelID, ok := panelIDFromPath(w, r)
	if !ok {
		return
	}
	if err := h.store.DeletePanel(r.Context(), panelID); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *DashboardHandler) PreviewPanel(w http.ResponseWriter, r *http.Request) {
	var req previewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.PromQL) == "" {
		writeError(w, http.StatusBadRequest, errors.New("promql is required"))
		return
	}
	data, err := h.prometheus.InstantQuery(r.Context(), req.PromQL)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func decodeDashboardRequest(w http.ResponseWriter, r *http.Request) (dashboardRequest, bool) {
	var req dashboardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return dashboardRequest{}, false
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, errors.New("title is required"))
		return dashboardRequest{}, false
	}
	return req, true
}

func decodePanelRequest(w http.ResponseWriter, r *http.Request) (panelRequest, bool) {
	var req panelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return panelRequest{}, false
	}
	req.Title = strings.TrimSpace(req.Title)
	req.PromQL = strings.TrimSpace(req.PromQL)
	if req.Title == "" || req.PromQL == "" {
		writeError(w, http.StatusBadRequest, errors.New("title and promql are required"))
		return panelRequest{}, false
	}
	if req.GridW <= 0 {
		req.GridW = 6
	}
	if req.GridH <= 0 {
		req.GridH = 4
	}
	if req.RefreshIntervalSeconds <= 0 {
		req.RefreshIntervalSeconds = 30
	}
	if req.VisualizationType == "" {
		req.VisualizationType = "line"
	}
	return req, true
}

func panelFromRequest(req panelRequest) store.Panel {
	return store.Panel{
		Title:                  req.Title,
		PromQL:                 req.PromQL,
		VisualizationType:      req.VisualizationType,
		GridX:                  req.GridX,
		GridY:                  req.GridY,
		GridW:                  req.GridW,
		GridH:                  req.GridH,
		RefreshIntervalSeconds: req.RefreshIntervalSeconds,
		SettingsJSON:           req.SettingsJSON,
	}
}

func dashboardIDFromPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid dashboard id"))
		return uuid.Nil, false
	}
	return id, true
}

func panelIDFromPath(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid panel id"))
		return uuid.Nil, false
	}
	return id, true
}

func writeStoreError(w http.ResponseWriter, err error) {
	if store.NotFound(err) {
		writeError(w, http.StatusNotFound, errors.New("not found"))
		return
	}
	writeError(w, http.StatusInternalServerError, err)
}
