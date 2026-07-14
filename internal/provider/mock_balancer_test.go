package provider

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sanchpet/sweb-go-sdk/balancer"
	"github.com/sanchpet/sweb-go-sdk/flex"
)

// mockBalancer is the path-scoped mock handler for the /balancer endpoint. It is
// registered with cloudMockHandlers from init() so the balancer acceptance test
// owns its wire behaviour without editing the shared method switch (whose
// index/create/edit/remove names collide across endpoints). The mockSweb lock is
// already held by the caller; state lives in m.balancers.
func init() { cloudMockHandlers = append(cloudMockHandlers, mockBalancer) }

func mockBalancer(m *mockSweb, path string, req rpcReq) (any, bool) {
	if !strings.Contains(path, "/balancer") {
		return nil, false
	}
	switch req.Method {
	case "index":
		// List unwraps {"ips":[…]}; report the current inventory.
		bals := m.balancers
		if bals == nil {
			bals = []balancer.Balancer{}
		}
		return map[string]any{"ips": bals}, true

	case "isCreateEnable":
		return 1, true // ordering is available (1 = enabled)

	case "getAvailableConfig":
		// Minimal valid Config the SDK decodes: one plan, one protocol.
		return map[string]any{
			"plans": []any{
				map[string]any{"id": "4298", "tag": "lb-mini", "title": "LB Mini", "price": 375},
			},
			"protocols": []any{
				map[string]any{"id": "tcp", "name": "TCP", "restrictions": []string{"tcp"}},
			},
			"descriptions": []any{},
		}, true

	case "create":
		var p balancerCreateParams
		_ = json.Unmarshal(req.Params, &p)
		m.seq2++
		m.balancers = append(m.balancers, balancer.Balancer{
			BillingID:     fmt.Sprintf("petrovpet2_balancer_%d", m.seq2),
			Name:          p.Alias,
			Type:          p.Type,
			PlanID:        flex.Int(p.PlanID),
			PlanName:      "LB Mini",
			Price:         flex.Int(375),
			Active:        true,
			CurrentAction: "", // idle immediately, so the create poll returns at once
			IPBalancer:    "203.0.113.60",
			Datacenter:    flex.Int(p.Datacenter),
			HealthCheck:   p.HealthCheck,
			ProxyProto:    p.ProxyProto,
			Keepalive:     p.Keepalive,
			SaveSession:   p.SaveSession,
			Servers:       serversFromWire(p.Servers),
			Rules:         rulesFromWire(p.Rules),
		})
		return 1, true // create sentinel (1 = procedure started)

	case "edit":
		var p balancerEditParams
		_ = json.Unmarshal(req.Params, &p)
		for i := range m.balancers {
			if m.balancers[i].BillingID == p.BillingID {
				m.balancers[i].Type = p.Type
				m.balancers[i].Name = p.Alias
				m.balancers[i].HealthCheck = p.HealthCheck
				m.balancers[i].ProxyProto = p.ProxyProto
				m.balancers[i].Keepalive = p.Keepalive
				m.balancers[i].SaveSession = p.SaveSession
				m.balancers[i].Servers = serversFromWire(p.Servers)
				m.balancers[i].Rules = rulesFromWire(p.Rules)
			}
		}
		return 1, true // edit sentinel

	case "remove":
		var p struct {
			BillingID string `json:"billingId"`
		}
		_ = json.Unmarshal(req.Params, &p)
		kept := m.balancers[:0]
		for _, b := range m.balancers {
			if b.BillingID != p.BillingID {
				kept = append(kept, b)
			}
		}
		m.balancers = kept
		return 1, true // remove sentinel
	}
	return nil, false
}

// balancerCreateParams mirrors the create params the SDK sends (map with the
// wire keys); Servers/Rules arrive as balancer.Server/Rule (they marshal to the
// same JSON the SDK reads back).
type balancerCreateParams struct {
	Datacenter  int               `json:"datacenter"`
	Type        string            `json:"type"`
	Servers     []balancer.Server `json:"servers"`
	Rules       []balancer.Rule   `json:"rules"`
	PlanID      int               `json:"planId"`
	HealthCheck bool              `json:"healthCheck"`
	ProxyProto  bool              `json:"proxyProto"`
	Keepalive   bool              `json:"keepalive"`
	SaveSession bool              `json:"saveSession"`
	Alias       string            `json:"alias"`
}

// balancerEditParams mirrors the edit params the SDK sends.
type balancerEditParams struct {
	BillingID   string            `json:"billingId"`
	Type        string            `json:"type"`
	Servers     []balancer.Server `json:"servers"`
	Rules       []balancer.Rule   `json:"rules"`
	HealthCheck bool              `json:"healthCheck"`
	ProxyProto  bool              `json:"proxyProto"`
	Keepalive   bool              `json:"keepalive"`
	SaveSession bool              `json:"saveSession"`
	Alias       string            `json:"alias"`
}

// serversFromWire/rulesFromWire echo the create/edit payload back into the stored
// balancer so a subsequent index reflects the requested topology.
func serversFromWire(servers []balancer.Server) []balancer.Server {
	if servers == nil {
		return []balancer.Server{}
	}
	return servers
}

func rulesFromWire(rules []balancer.Rule) []balancer.Rule {
	if rules == nil {
		return []balancer.Rule{}
	}
	return rules
}
