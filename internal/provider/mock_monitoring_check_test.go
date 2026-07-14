package provider

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/sanchpet/sweb-go-sdk/monitoring/checks"
)

// init registers the path-scoped mock handler for /monitoring/checks so the
// acceptance test can exercise the resource without editing the shared switch.
func init() { cloudMockHandlers = append(cloudMockHandlers, mockMonitoringCheck) }

// mockMonitoringCheck is the in-memory stand-in for the /monitoring/checks
// endpoint. It owns the create/index/edit/activate/deactivate/remove methods and
// defers everything else (returns false). Only the /monitoring/checks path is
// claimed — the more specific substring leaves /monitoring/contacts to its own
// handler. The mock lock is already held by the caller; do not lock here.
func mockMonitoringCheck(m *mockSweb, path string, req rpcReq) (any, bool) {
	if !strings.Contains(path, "/monitoring/checks") {
		return nil, false
	}

	switch req.Method {
	case "index":
		list := m.checks
		if list == nil {
			list = []checks.Check{}
		}
		// CheckList shape: {list:[...], filterInfo:{...}}.
		return map[string]any{
			"list": list,
			"filterInfo": map[string]any{
				"page": 1, "perPage": len(list), "totalCount": len(list),
			},
		}, true

	case "create":
		var p struct {
			Type     int    `json:"type"`
			Target   string `json:"target"`
			Name     string `json:"name"`
			Interval int    `json:"interval"`
		}
		_ = json.Unmarshal(req.Params, &p)
		m.seq2++
		m.checks = append(m.checks, checks.Check{
			ID:     strconv.Itoa(m.seq2),
			Name:   p.Name,
			Type:   strconv.Itoa(p.Type),
			Status: true, // a new check is active by default
		})
		return 1, true // actionOne sentinel

	case "edit":
		var p struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		_ = json.Unmarshal(req.Params, &p)
		for i := range m.checks {
			if m.checks[i].ID == strconv.Itoa(p.ID) {
				m.checks[i].Name = p.Name
			}
		}
		return 1, true

	case "activate", "deactivate":
		var p struct {
			ID int `json:"id"`
		}
		_ = json.Unmarshal(req.Params, &p)
		for i := range m.checks {
			if m.checks[i].ID == strconv.Itoa(p.ID) {
				m.checks[i].Status = req.Method == "activate"
			}
		}
		return 1, true

	case "remove":
		var p struct {
			ID int `json:"id"`
		}
		_ = json.Unmarshal(req.Params, &p)
		kept := m.checks[:0]
		for _, c := range m.checks {
			if c.ID != strconv.Itoa(p.ID) {
				kept = append(kept, c)
			}
		}
		m.checks = kept
		return 1, true
	}

	return nil, false
}
