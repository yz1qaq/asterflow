package httpapi

import (
	"net/http"
	"strings"

	"github.com/yz1qaq/asterflow/crossborder/internal/service"
)

func New(svc *service.Service) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"project": "crossborder",
		})
	})

	mux.HandleFunc("POST /tools/get_order", func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		var in struct {
			OrderID string `json:"order_id"`
		}

		if !decode(w, r, &in) {
			return
		}

		order, err := svc.GetOrder(r.Context(), in.OrderID)
		respond(w, order, err)
	})

	mux.HandleFunc("POST /tools/get_inventory", func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		var in struct {
			SKU string `json:"sku"`
		}

		if !decode(w, r, &in) {
			return
		}

		in.SKU = strings.TrimSpace(in.SKU)

		balances, err := svc.Inventory(r.Context(), in.SKU)
		respond(w, map[string]any{
			"sku":      in.SKU,
			"balances": balances,
		}, err)
	})

	return mux
}
