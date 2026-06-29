package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	mu    sync.Mutex
	nodes []sweb.VPS
	seq   int
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
			DiskGB:       15,
			PlanID:       strconv.Itoa(p.VPSPlanID),
			OSDistrID:    p.DistributiveID,
			DatacenterID: strconv.Itoa(p.Datacenter),
		})
		result = map[string]bool{"ok": true}
	case "index":
		result = m.nodes
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
