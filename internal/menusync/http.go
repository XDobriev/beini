package menusync

import (
	"encoding/json"
	"net/http"

	"beini-backend/internal/catalog"
)

// MenuHandler serves the same version used for order validation; registration is
// intentionally left to the application's integrator. It never calls iiko.
func MenuHandler(store *catalog.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "method_not_allowed"})
			return
		}
		view := store.View()
		if view.Version == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "menu_unavailable"})
			return
		}
		_ = json.NewEncoder(w).Encode(view)
	})
}
