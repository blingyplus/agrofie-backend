package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// GraphQLHandler serves a scaffold GraphQL API backed by Resolver.
// Schema source of truth: graph/schema.graphqls (gqlgen generate can replace this later).
type GraphQLHandler struct {
	Resolver *Resolver
}

type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type gqlResponse struct {
	Data   map[string]any `json:"data,omitempty"`
	Errors []gqlError     `json:"errors,omitempty"`
}

type gqlError struct {
	Message string `json:"message"`
}

func (h *GraphQLHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(playgroundHTML))
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req gqlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, gqlResponse{Errors: []gqlError{{Message: err.Error()}}})
		return
	}

	data, err := h.execute(r.Context(), req.Query, req.Variables)
	if err != nil {
		writeJSON(w, http.StatusOK, gqlResponse{Errors: []gqlError{{Message: err.Error()}}})
		return
	}
	writeJSON(w, http.StatusOK, gqlResponse{Data: data})
}

func writeJSON(w http.ResponseWriter, status int, body gqlResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (h *GraphQLHandler) execute(ctx context.Context, query string, vars map[string]any) (map[string]any, error) {
	q := strings.TrimSpace(query)
	activeOnly := boolVar(vars, "activeOnly", true)
	out := map[string]any{}

	// Scaffold dispatcher: matches operations declared in graph/schema.graphqls.
	switch {
	case strings.Contains(q, "health"):
		out["health"] = h.Resolver.Health(ctx)
	}
	if strings.Contains(q, "countries") {
		rows, err := h.Resolver.Countries(ctx, activeOnly)
		if err != nil {
			return nil, err
		}
		out["countries"] = rows
	}
	if strings.Contains(q, "geoPlaces") {
		countryCode := stringVar(vars, "countryCode", "GH")
		var parent *string
		if v, ok := vars["parentCode"].(string); ok {
			parent = &v
		}
		rows, err := h.Resolver.GeoPlaces(ctx, countryCode, parent, activeOnly)
		if err != nil {
			return nil, err
		}
		out["geoPlaces"] = rows
	}
	if strings.Contains(q, "genres") && !strings.Contains(q, "updateLookupName") {
		rows, err := h.Resolver.Genres(ctx, activeOnly)
		if err != nil {
			return nil, err
		}
		out["genres"] = rows
	}
	if strings.Contains(q, "talentTypes") {
		rows, err := h.Resolver.TalentTypes(ctx, activeOnly)
		if err != nil {
			return nil, err
		}
		out["talentTypes"] = rows
	}
	if strings.Contains(q, "eventTypes") {
		rows, err := h.Resolver.EventTypes(ctx, activeOnly)
		if err != nil {
			return nil, err
		}
		out["eventTypes"] = rows
	}
	if strings.Contains(q, "languages") {
		rows, err := h.Resolver.Languages(ctx, activeOnly)
		if err != nil {
			return nil, err
		}
		out["languages"] = rows
	}
	if strings.Contains(q, "roles") {
		rows, err := h.Resolver.Roles(ctx, activeOnly)
		if err != nil {
			return nil, err
		}
		out["roles"] = rows
	}
	if strings.Contains(q, "bookingStatuses") {
		rows, err := h.Resolver.BookingStatuses(ctx, activeOnly)
		if err != nil {
			return nil, err
		}
		out["bookingStatuses"] = rows
	}
	if strings.Contains(q, "updateLookupName") {
		table := stringVar(vars, "table", "")
		code := stringVar(vars, "code", "")
		name := stringVar(vars, "name", "")
		row, err := h.Resolver.UpdateLookupName(ctx, table, code, name)
		if err != nil {
			return nil, err
		}
		out["updateLookupName"] = row
	}
	if strings.Contains(q, "setLookupActive") {
		table := stringVar(vars, "table", "")
		code := stringVar(vars, "code", "")
		active := boolVar(vars, "isActive", true)
		row, err := h.Resolver.SetLookupActive(ctx, table, code, active)
		if err != nil {
			return nil, err
		}
		out["setLookupActive"] = row
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("unsupported or empty GraphQL operation; see graph/schema.graphqls")
	}
	return out, nil
}

func boolVar(vars map[string]any, key string, fallback bool) bool {
	if vars == nil {
		return fallback
	}
	v, ok := vars[key]
	if !ok || v == nil {
		return fallback
	}
	b, ok := v.(bool)
	if !ok {
		return fallback
	}
	return b
}

func stringVar(vars map[string]any, key, fallback string) string {
	if vars == nil {
		return fallback
	}
	v, ok := vars[key]
	if !ok || v == nil {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	return s
}

const playgroundHTML = `<!DOCTYPE html>
<html>
<head><title>Agrofie GraphQL</title>
<style>body{font-family:system-ui;margin:2rem}textarea{width:100%;height:200px}pre{background:#111;color:#eee;padding:1rem}</style>
</head>
<body>
<h1>Agrofie GraphQL Playground (scaffold)</h1>
<p>POST JSON to <code>/graphql</code>. Example:</p>
<textarea id="q">{ health { status service auth booking payments } }</textarea>
<button onclick="run()">Run</button>
<pre id="out"></pre>
<script>
async function run(){
  const q = document.getElementById('q').value;
  const res = await fetch('/graphql',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({query:q})});
  document.getElementById('out').textContent = JSON.stringify(await res.json(),null,2);
}
</script>
</body></html>`
