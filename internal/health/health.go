package health

import (
	"encoding/json"
	"net/http"
	"time"
)

type Response struct {
	Status    string    `json:"status"`
	Service   string    `json:"service"`
	Timestamp time.Time `json:"timestamp"`
}

func Handler(serviceName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Response{
			Status:    "ok",
			Service:   serviceName,
			Timestamp: time.Now().UTC(),
		})
	}
}
