package provider

import (
	"encoding/json"
	"fmt"
	"strings"

	swebip "github.com/sanchpet/sweb-go-sdk/ip"
)

// mockVPSIP is the path-scoped mock handler for the /vps/ip endpoint: the IP
// inventory (index), the private-network attachment it also reports, and
// ordering/releasing additional public IPs. It owns the whole endpoint because
// its method names (index/add/remove) collide with /sites and /vps in the shared
// method switch. The mockSweb lock is already held by the caller.
func init() { cloudMockHandlers = append(cloudMockHandlers, mockVPSIP) }

func mockVPSIP(m *mockSweb, path string, req rpcReq) (any, bool) {
	if !strings.HasSuffix(path, "/vps/ip") {
		return nil, false
	}
	var p struct {
		BillingID string `json:"billingId"`
		IP        string `json:"ip"`
		Number    int    `json:"number"`
	}
	_ = json.Unmarshal(req.Params, &p)

	switch req.Method {
	case "index":
		local := []any{}
		if m.localAttached[p.BillingID] {
			local = []any{map[string]string{"ip": "10.0.0.24", "mac": "00:16:3e:aa:bb:cc", "mask": "10.0.0.0/27"}}
		}
		ips := m.publicIPs[p.BillingID]
		if ips == nil {
			ips = []swebip.Address{}
		}
		return map[string]any{
			"ips": ips, "protected_ips": []any{}, "local_ip": local,
			"vps": map[string]any{
				"billingId": p.BillingID, "isEmpty": "0", "ordered_ip_count": len(ips),
			},
		}, true

	case "add":
		// SpaceWeb refuses any operation while another one runs on the VPS.
		if m.ipBusyOrders > 0 {
			m.ipBusyOrders--
			return rpcError{Code: invalidParamsCode, Message: "Выполняется другая операция"}, true
		}
		if m.publicIPs == nil {
			m.publicIPs = map[string][]swebip.Address{}
		}
		for range max(p.Number, 1) {
			m.ipSeq++
			m.publicIPs[p.BillingID] = append(m.publicIPs[p.BillingID], swebip.Address{
				IP:      fmt.Sprintf("203.0.113.%d", 100+m.ipSeq),
				Gateway: "203.0.113.1",
				Netmask: "203.0.113.0/24",
			})
		}
		return map[string]any{"ok": true}, true

	case "remove":
		// An IP ordered less than 24h ago cannot be released. The knob is a
		// countdown, so the same address releases fine on the next attempt —
		// which is what "retriable tomorrow" means for the test's own cleanup.
		if m.ipRemoveLocked > 0 {
			m.ipRemoveLocked--
			return rpcError{
				Code:    invalidParamsCode,
				Message: "Отказаться от IP-адреса можно через 24 часа после заказа",
			}, true
		}
		kept := make([]swebip.Address, 0, len(m.publicIPs[p.BillingID]))
		for _, a := range m.publicIPs[p.BillingID] {
			if a.IP != p.IP {
				kept = append(kept, a)
			}
		}
		m.publicIPs[p.BillingID] = kept
		return 1, true
	}
	return nil, false
}
