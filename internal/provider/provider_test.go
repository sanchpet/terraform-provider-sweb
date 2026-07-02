package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	sweb "github.com/sanchpet/sweb-go-sdk"
)

// testAccProtoV6ProviderFactories wires the in-process provider for acceptance
// tests (no separate plugin process).
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"sweb": providerserver.NewProtocol6WithError(New("test")()),
}

// mockSweb is a stateful in-memory stand-in for the SpaceWeb JSON-RPC API. It is
// enough to exercise the full create → list-diff → poll → read → import → remove
// lifecycle without touching (or billing) the real service.
type mockSweb struct {
	mu            sync.Mutex
	nodes         []sweb.VPS
	seq           int
	localAttached map[string]bool // billingId → attached to the local network
}

type rpcReq struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func newMockSweb() *httptest.Server {
	m := &mockSweb{}
	return httptest.NewServer(http.HandlerFunc(m.handle))
}

func (m *mockSweb) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req rpcReq
	_ = json.Unmarshal(body, &req)

	m.mu.Lock()
	defer m.mu.Unlock()

	var result any
	switch req.Method {
	case "getToken":
		result = "test-token"
	case "getConstructorPlanId":
		result = 379
	case "create":
		var p sweb.CreateVPSRequest
		_ = json.Unmarshal(req.Params, &p)
		m.seq++
		m.nodes = append(m.nodes, sweb.VPS{
			BillingID:    fmt.Sprintf("petrovpet2_vps_%d", m.seq),
			UID:          fmt.Sprintf("uid-%d", m.seq),
			Name:         p.Alias,
			IP:           "203.0.113.50",
			IsRunning:    1,
			CPU:          2,
			RAM:          6,
			Disk:         "15 ГБ", // index reports disk as a localized string, not diskGb
			PlanID:       sweb.FlexInt(p.VPSPlanID),
			OSDistrID:    sweb.FlexInt(p.DistributiveID),
			DatacenterID: strconv.Itoa(p.Datacenter),
		})
		result = map[string]bool{"ok": true}
	case "index":
		if strings.HasSuffix(r.URL.Path, "/vps/ip") {
			// IP inventory (endpoint /vps/ip): report the local IP if attached.
			var p map[string]string
			_ = json.Unmarshal(req.Params, &p)
			local := []any{}
			if m.localAttached[p["billingId"]] {
				local = []any{map[string]string{"ip": "10.0.0.24", "mac": "00:16:3e:aa:bb:cc", "mask": "10.0.0.0/27"}}
			}
			result = map[string]any{
				"ips": []any{}, "protected_ips": []any{}, "local_ip": local,
				"vps": map[string]any{"billingId": p["billingId"], "isEmpty": "0", "ordered_ip_count": "1"},
			}
		} else {
			result = m.nodes
		}
	case "addLocal":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		if m.localAttached == nil {
			m.localAttached = map[string]bool{}
		}
		m.localAttached[p["billingId"]] = true
		result = 1
	case "removeLocal":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		delete(m.localAttached, p["billingId"])
		result = 1
	case "remove":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		kept := m.nodes[:0]
		for _, n := range m.nodes {
			if n.BillingID != p["billingId"] {
				kept = append(kept, n)
			}
		}
		m.nodes = kept
		result = map[string]bool{"ok": true}
	case "rename":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		for i := range m.nodes {
			if m.nodes[i].BillingID == p["billingId"] {
				m.nodes[i].Name = p["alias"]
			}
		}
		result = 1
	case "changePlan":
		var p struct {
			BillingID string `json:"billingId"`
			PlanID    int    `json:"planId"`
		}
		_ = json.Unmarshal(req.Params, &p)
		for i := range m.nodes {
			if m.nodes[i].BillingID == p.BillingID {
				m.nodes[i].PlanID = sweb.FlexInt(p.PlanID)
			}
		}
		result = 1
	default:
		result = map[string]bool{"ok": true}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"result": result})
}

// TestAtoiOr is a cheap unit test (no TF_ACC) for the id-parsing helper.
func TestAtoiOr(t *testing.T) {
	if got := atoiOr("379", 0); got != 379 {
		t.Fatalf("atoiOr(379) = %d, want 379", got)
	}
	if got := atoiOr("not-a-number", 7); got != 7 {
		t.Fatalf("atoiOr(garbage) = %d, want fallback 7", got)
	}
}
