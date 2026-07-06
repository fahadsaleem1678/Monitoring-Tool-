package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"monitoring-tool/backend/internal/auth"
	"monitoring-tool/backend/internal/store"
)

type AuthHandler struct {
	store *store.Store
	auth  *auth.Service
}

func NewAuthHandler(store *store.Store, auth *auth.Service) *AuthHandler {
	return &AuthHandler{store: store, auth: auth}
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	user, err := h.store.UserByUsername(r.Context(), req.Username)
	if err != nil {
		writeError(w, http.StatusUnauthorized, errInvalidCredentials)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, errInvalidCredentials)
		return
	}

	authUser := auth.User{ID: user.ID, Username: user.Username, Role: user.Role}
	token, err := h.auth.Sign(authUser, 24*time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  authUser,
	})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, errUnauthorized)
		return
	}

	freshUser, err := h.store.UserByID(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, errUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": auth.User{ID: freshUser.ID, Username: freshUser.Username, Role: freshUser.Role},
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
