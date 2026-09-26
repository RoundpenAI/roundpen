package assistant

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/httpx"
)

func (h *Handler) weixinBegin(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if _, err := h.ownedAssistant(r.Context(), user, r.PathValue("id")); err != nil {
		writeAssistantErr(w, err)
		return
	}
	var body struct {
		APIURL string `json:"apiUrl"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)
	begin, err := WeixinBeginQR(r.Context(), body.APIURL)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, begin)
}

func (h *Handler) weixinPoll(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id := r.PathValue("id")
	a, err := h.ownedAssistant(r.Context(), user, id)
	if err != nil {
		writeAssistantErr(w, err)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var body struct {
		QRKey  string `json:"qrKey"`
		APIURL string `json:"apiUrl"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || strings.TrimSpace(body.QRKey) == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "qrKey required")
		return
	}
	st, err := WeixinPollQR(r.Context(), body.APIURL, body.QRKey)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if st.Status != "confirmed" {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": st.Status})
		return
	}
	if st.BotToken == "" {
		httpx.WriteErr(w, http.StatusBadGateway, "weixin: confirmed without token")
		return
	}

	channels := a.ImChannels
	if channels == nil {
		channels = ImChannels{}
	}
	prev := channels["weixin"]
	ch := ImChannel{
		Enabled:   true,
		Token:     st.BotToken,
		BaseURL:   st.BaseURL,
		AccountID: st.IlinkBotID,
		AllowFrom: prev.AllowFrom,
	}
	if ch.AllowFrom == "" && st.IlinkUserID != "" {
		ch.AllowFrom = st.IlinkUserID
	}
	if ch.BaseURL == "" {
		ch.BaseURL = strings.TrimSpace(body.APIURL)
	}
	channels["weixin"] = ch
	updated, err := h.Store.Update(r.Context(), id, UpdateInput{ImChannels: &channels})
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.syncIM(r.Context(), updated)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"status":     "confirmed",
		"connected":  true,
		"accountId":  st.IlinkBotID,
		"imChannels": SanitizeImChannels(updated.ImChannels),
	})
}
