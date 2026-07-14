package provider

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sanchpet/sweb-go-sdk/dbaas"
	"github.com/sanchpet/sweb-go-sdk/flex"
)

// init registers the DBaaS mock handler with the shared path-scoped registry, so
// the managed-database methods (createInstance/index/editInstance/removeInstance
// + the create-page lookups) don't collide with the main method switch or the
// other cloud-tier handlers.
func init() { cloudMockHandlers = append(cloudMockHandlers, mockDBaaS) }

// mockDBaaS is the in-memory stand-in for the /dbaas endpoint. It owns any call on
// a path containing "/dbaas" and defers everything else. State lives in m.dbaas
// (reset per newMockSweb); the shared mutex is already held by the caller.
func mockDBaaS(m *mockSweb, path string, req rpcReq) (any, bool) {
	if !strings.Contains(path, "/dbaas") {
		return nil, false
	}

	switch req.Method {
	case "createInstance":
		var p dbaas.CreateInstanceRequest
		_ = json.Unmarshal(req.Params, &p)
		m.seq2++
		m.dbaas = append(m.dbaas, dbaas.Instance{
			BillingID:     fmt.Sprintf("petrovpet2_dbaas_%d", m.seq2),
			Name:          p.DisplayName,
			DisplayName:   p.DisplayName,
			Engine:        p.EngineType,
			Status:        "running",
			CurrentAction: "", // idle immediately so the create poll returns at once
			Active:        true,
			IP:            "203.0.113.70:5432",
			Plan:          dbaas.Plan{ID: flex.Int(p.PlanID)},
			Endpoints:     []dbaas.Endpoint{{Type: "rw", IP: "203.0.113.70", Port: 5432}},
			Users:         usersFrom(p.Users),
		})
		// CreateInstance decodes into json.RawMessage; the documented shape is
		// {"extendedResult":{code,message,data}}. Any well-formed JSON suffices.
		return map[string]any{"extendedResult": map[string]any{"code": 0, "message": "ok"}}, true

	case "index":
		// List decodes into dbaas.Index — the instances slice plus beta-quota fields.
		insts := m.dbaas
		if insts == nil {
			insts = []dbaas.Instance{}
		}
		return map[string]any{
			"instances":   insts,
			"total_count": len(insts),
			"max_count":   10,
			"can_create":  true,
		}, true

	case "editInstance":
		var p dbaas.EditInstanceRequest
		_ = json.Unmarshal(req.Params, &p)
		for i := range m.dbaas {
			if m.dbaas[i].BillingID != p.BillingID {
				continue
			}
			if p.PlanID != 0 {
				m.dbaas[i].Plan.ID = flex.Int(p.PlanID)
			}
			if p.DisplayName != "" {
				m.dbaas[i].DisplayName = p.DisplayName
				m.dbaas[i].Name = p.DisplayName
			}
			if p.Users != nil {
				m.dbaas[i].Users = usersFrom(p.Users)
			}
		}
		return 1, true // editInstance sentinel: integer 1

	case "removeInstance":
		var p struct {
			BillingID string `json:"billingId"`
		}
		_ = json.Unmarshal(req.Params, &p)
		kept := m.dbaas[:0]
		for _, inst := range m.dbaas {
			if inst.BillingID != p.BillingID {
				kept = append(kept, inst)
			}
		}
		m.dbaas = kept
		return 1, true // documented bare 1 on success

	case "getAvailableConfig":
		return map[string]any{
			"plans":   []any{map[string]any{"id": 100, "name": "start", "cpu": 1, "memory": 2, "storage": 20}},
			"engines": map[string]any{"PostgreSQL": []any{map[string]any{"name": "PostgreSQL", "version": "16"}}},
			"kit":     map[string]any{},
		}, true

	case "getConstructorPlanId":
		return 100, true
	}

	return nil, false
}

// usersFrom maps request credentials to the API's user shape (only the name is
// exposed on read; passwords are never echoed back).
func usersFrom(creds []dbaas.UserCredentials) []dbaas.User {
	out := make([]dbaas.User, 0, len(creds))
	for _, c := range creds {
		out = append(out, dbaas.User{Name: c.Name})
	}
	return out
}
