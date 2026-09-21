package settingitems

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/audit"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// EnvRebuilder rebuilds a user's environment after a slot selection changes.
// Slots marked RebuildsEnv only take effect when their sandbox is created.
type EnvRebuilder interface {
	RebuildForSlot(ctx context.Context, username, slot string) (status string, environment any, err error)
}

// Handler serves the item and binding endpoints.
type Handler struct {
	Cat  *Catalog
	Envs EnvRebuilder
}

// Mount registers admin and per-user routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/setting-schema", h.schema)

	mux.HandleFunc("GET /v1/admin/setting-items", auth.RequireAdmin(h.adminList))
	mux.HandleFunc("POST /v1/admin/setting-items", auth.RequireAdmin(h.adminCreate))
	mux.HandleFunc("PUT /v1/admin/setting-items/order", auth.RequireAdmin(h.adminOrder))
	mux.HandleFunc("PUT /v1/admin/setting-items/{kind}/{id}", auth.RequireAdmin(h.adminSave))
	mux.HandleFunc("DELETE /v1/admin/setting-items/{kind}/{id}", auth.RequireAdmin(h.adminDelete))
	mux.HandleFunc("GET /v1/admin/setting-bindings", auth.RequireAdmin(h.adminBindings))
	mux.HandleFunc("PUT /v1/admin/setting-bindings/{slot}", auth.RequireAdmin(h.adminSetBinding))
	mux.HandleFunc("DELETE /v1/admin/setting-bindings/{slot}", auth.RequireAdmin(h.adminClearBinding))

	mux.HandleFunc("GET /v1/me/setting-items", h.userList)
	mux.HandleFunc("GET /v1/me/setting-bindings", h.userBindings)
	mux.HandleFunc("PUT /v1/me/setting-bindings/{slot}", h.userSetBinding)
	mux.HandleFunc("DELETE /v1/me/setting-bindings/{slot}", h.userClearBinding)
}

type schemaResp struct {
	Kinds []KindDef `json:"kinds"`
	Slots []SlotDef `json:"slots"`
}

// schema lets any authenticated caller render item forms and slot pickers
// without duplicating the definitions in TypeScript.
func (h *Handler) schema(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, schemaResp{
		Kinds: h.Cat.Registry().Kinds(),
		Slots: h.Cat.Registry().Slots(),
	})
}

// adminList returns stored items, disabled ones included, with secrets masked.
func (h *Handler) adminList(w http.ResponseWriter, r *http.Request) {
	kind := Kind(strings.TrimSpace(r.URL.Query().Get("kind")))
	if kind != "" {
		if _, ok := h.Cat.Registry().Kind(kind); !ok {
			httpx.WriteErr(w, http.StatusBadRequest, "unknown kind")
			return
		}
	}
	items, err := h.Cat.store.List(r.Context(), kind)
	if err != nil {
		httpx.WriteErrOrInternal(w, r, err, nil)
		return
	}
	out := make([]Item, 0, len(items))
	for _, it := range items {
		def, ok := h.Cat.Registry().Kind(it.Kind)
		if !ok {
			continue
		}
		out = append(out, it.AdminView(def))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) adminCreate(w http.ResponseWriter, r *http.Request) {
	it, err := decodeItem(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	saved, err := h.Cat.Save(r.Context(), it)
	if err != nil {
		writeSaveErr(w, r, err)
		return
	}
	audit.Record(r.Context(), "setting_items.create")
	def, _ := h.Cat.Registry().Kind(saved.Kind)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"item": saved.AdminView(def)})
}

func (h *Handler) adminSave(w http.ResponseWriter, r *http.Request) {
	it, err := decodeItem(r)
	if err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	it.Kind = Kind(r.PathValue("kind"))
	it.ID = r.PathValue("id")
	saved, err := h.Cat.Save(r.Context(), it)
	if err != nil {
		writeSaveErr(w, r, err)
		return
	}
	audit.Record(r.Context(), "setting_items.update")
	def, _ := h.Cat.Registry().Kind(saved.Kind)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"item": saved.AdminView(def)})
}

func (h *Handler) adminOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind Kind     `json:"kind"`
		IDs  []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := h.Cat.Reorder(r.Context(), body.Kind, body.IDs); err != nil {
		writeSaveErr(w, r, err)
		return
	}
	audit.Record(r.Context(), "setting_items.reorder")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"kind": body.Kind, "ids": body.IDs})
}

func (h *Handler) adminDelete(w http.ResponseWriter, r *http.Request) {
	kind := Kind(r.PathValue("kind"))
	id := r.PathValue("id")
	force := r.URL.Query().Get("force") == "true"
	err := h.Cat.Delete(r.Context(), kind, id, force)
	var bound *BoundError
	if errors.As(err, &bound) {
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{
			"error":      err.Error(),
			"boundSlots": bound.Slots,
		})
		return
	}
	if errors.Is(err, storage.ErrNotFound) {
		httpx.WriteErr(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeSaveErr(w, r, err)
		return
	}
	audit.Record(r.Context(), "setting_items.delete")
	w.WriteHeader(http.StatusNoContent)
}

type bindingEntry struct {
	Slot   string         `json:"slot"`
	Kind   Kind           `json:"kind"`
	ItemID string         `json:"itemId,omitempty"`
	Params map[string]any `json:"params,omitempty"`
}

// adminBindings lists the global defaults for every registered slot.
func (h *Handler) adminBindings(w http.ResponseWriter, r *http.Request) {
	snap := h.Cat.Snapshot()
	entries := []bindingEntry{}
	for _, def := range h.Cat.Registry().Slots() {
		e := bindingEntry{Slot: def.Key, Kind: def.Kind}
		if b, ok := snap.Binding(GlobalScope, def.Key); ok {
			e.ItemID, e.Params = b.ItemID, b.Params
		}
		entries = append(entries, e)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"bindings": entries,
		"slots":    h.Cat.Registry().Slots(),
		"kinds":    h.Cat.Registry().Kinds(),
	})
}

func (h *Handler) adminSetBinding(w http.ResponseWriter, r *http.Request) {
	h.applyBinding(w, r, GlobalScope, "")
}

func (h *Handler) adminClearBinding(w http.ResponseWriter, r *http.Request) {
	slot := r.PathValue("slot")
	if _, ok := h.Cat.Registry().Slot(slot); !ok {
		httpx.WriteErr(w, http.StatusNotFound, "unknown slot")
		return
	}
	if err := h.Cat.DeleteBinding(r.Context(), GlobalScope, slot); err != nil {
		writeSaveErr(w, r, err)
		return
	}
	audit.Record(r.Context(), "setting_bindings.clear")
	w.WriteHeader(http.StatusNoContent)
}

type userBindingEntry struct {
	bindingEntry
	EffectiveItemID string         `json:"effectiveItemId,omitempty"`
	EffectiveParams map[string]any `json:"effectiveParams,omitempty"`
	Source          string         `json:"source,omitempty"`
}

// userBindings reports every user-overridable slot with its effective value.
func (h *Handler) userBindings(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	snap := h.Cat.Snapshot()
	out := map[string]userBindingEntry{}
	for _, def := range h.Cat.Registry().Slots() {
		if !def.UserOverride {
			continue
		}
		entry := userBindingEntry{bindingEntry: bindingEntry{Slot: def.Key, Kind: def.Kind}}
		if b, ok := snap.Binding(UserScope(user.Username), def.Key); ok {
			entry.ItemID, entry.Params = b.ItemID, b.Params
		}
		if res := snap.Resolve(def.Key, user.Username); res.OK {
			entry.EffectiveItemID = res.Item.ID
			entry.EffectiveParams = res.Params
			entry.Source = res.Source
		}
		out[def.Key] = entry
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"slots": out,
		"kinds": h.Cat.Registry().Kinds(),
	})
}

func (h *Handler) userSetBinding(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	h.applyBinding(w, r, UserScope(user.Username), user.Username)
}

func (h *Handler) userClearBinding(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	slot := r.PathValue("slot")
	def, err := h.overridableSlot(slot)
	if err != nil {
		writeSaveErr(w, r, err)
		return
	}
	scope := UserScope(user.Username)
	prev, hadPrev := h.Cat.Snapshot().Binding(scope, slot)
	if err := h.Cat.DeleteBinding(r.Context(), scope, slot); err != nil {
		writeSaveErr(w, r, err)
		return
	}
	audit.Record(r.Context(), "setting_bindings.clear")
	resp := map[string]any{"slot": slot, "itemId": ""}
	if hadPrev && prev.ItemID != "" {
		h.attachRebuild(r.Context(), user.Username, def, resp)
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) overridableSlot(slot string) (SlotDef, error) {
	def, ok := h.Cat.Registry().Slot(slot)
	if !ok {
		return SlotDef{}, fmt.Errorf("%w: %q", ErrSlotUnknown, slot)
	}
	if !def.UserOverride {
		return SlotDef{}, fmt.Errorf("slot %q cannot be overridden per user", slot)
	}
	return def, nil
}

// applyBinding handles PUT for both scopes; username is "" for the global scope.
func (h *Handler) applyBinding(w http.ResponseWriter, r *http.Request, scope, username string) {
	slot := r.PathValue("slot")
	def, ok := h.Cat.Registry().Slot(slot)
	if !ok {
		httpx.WriteErr(w, http.StatusNotFound, "unknown slot")
		return
	}
	if scope != GlobalScope && !def.UserOverride {
		httpx.WriteErr(w, http.StatusBadRequest, "slot cannot be overridden per user")
		return
	}
	var body struct {
		ItemID string         `json:"itemId"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	prev, hadPrev := h.Cat.Snapshot().Binding(scope, slot)
	binding := Binding{Scope: scope, Slot: slot, ItemID: strings.TrimSpace(body.ItemID), Params: body.Params}
	switch {
	case binding.ItemID == "":
		if err := h.Cat.DeleteBinding(r.Context(), scope, slot); err != nil {
			writeSaveErr(w, r, err)
			return
		}
	default:
		if err := h.Cat.SetBinding(r.Context(), binding); err != nil {
			writeSaveErr(w, r, err)
			return
		}
	}
	audit.Record(r.Context(), "setting_bindings.update")
	resp := map[string]any{"slot": slot, "itemId": binding.ItemID}
	if scope != GlobalScope && (!hadPrev || prev.ItemID != binding.ItemID) {
		h.attachRebuild(r.Context(), username, def, resp)
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// attachRebuild recreates the affected sandbox so the new selection applies.
// A rebuild failure is reported without failing the request: the preference
// is saved and the environment can be rebuilt from the environments page.
func (h *Handler) attachRebuild(ctx context.Context, username string, def SlotDef, resp map[string]any) {
	if !def.RebuildsEnv || h.Envs == nil || username == "" {
		return
	}
	status, env, err := h.Envs.RebuildForSlot(ctx, username, def.Key)
	if err != nil {
		resp["rebuildError"] = err.Error()
		return
	}
	resp["status"] = status
	resp["environment"] = env
}

func (h *Handler) userList(w http.ResponseWriter, r *http.Request) {
	kind := Kind(strings.TrimSpace(r.URL.Query().Get("kind")))
	def, ok := h.Cat.Registry().Kind(kind)
	if !ok {
		httpx.WriteErr(w, http.StatusBadRequest, "unknown kind")
		return
	}
	views := []Item{}
	for _, it := range h.Cat.Snapshot().Items(kind, false) {
		views = append(views, it.SelectView(def))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": views})
}

func decodeItem(r *http.Request) (Item, error) {
	var it Item
	if err := json.NewDecoder(r.Body).Decode(&it); err != nil {
		return Item{}, err
	}
	return it, nil
}

func writeSaveErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrKindUnknown), errors.Is(err, ErrSlotUnknown), errors.Is(err, storage.ErrNotFound):
		httpx.WriteErr(w, http.StatusBadRequest, err.Error())
	default:
		httpx.WriteErrOrInternal(w, r, err, nil)
	}
}
