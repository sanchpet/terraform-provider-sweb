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
	localAttached map[string]bool                // billingId → attached to the local network
	ptr           map[string]string              // ip → PTR record
	backupSet     map[string]sweb.BackupSettings // billingId → auto-backup schedule
	subdomains    map[string][]string            // domain → subdomain machine labels
	redirect      map[string]string              // domain → redirect URL
	dnsRecords    map[string][]sweb.DNSRecord    // domain → DNS zone records
}

// editDNS applies an add/del to the mock zone, mirroring the real API's per-type
// index addressing (host in Domain for TXT, Name otherwise). okSentinel is the
// method's success value (integer 1 for editMx, boolean true for the rest).
func (m *mockSweb) editDNS(raw json.RawMessage, fixedType string, okSentinel any) any {
	var p struct {
		Domain    string `json:"domain"`
		Action    string `json:"action"`
		Index     int    `json:"index"`
		Name      string `json:"name"`
		Type      string `json:"type"`
		Value     string `json:"value"`
		Priority  int    `json:"priority"`
		SubDomain string `json:"subDomain"`
	}
	_ = json.Unmarshal(raw, &p)
	if m.dnsRecords == nil {
		m.dnsRecords = map[string][]sweb.DNSRecord{}
	}
	if p.Action == "del" {
		kept := m.dnsRecords[p.Domain][:0]
		for _, rec := range m.dnsRecords[p.Domain] {
			if strings.EqualFold(rec.Type, p.Type) && int(rec.Index) == p.Index {
				continue
			}
			kept = append(kept, rec)
		}
		m.dnsRecords[p.Domain] = kept
		return okSentinel
	}
	rtype := fixedType
	if rtype == "" {
		rtype = strings.ToUpper(p.Type)
	}
	idx := 0
	for _, rec := range m.dnsRecords[p.Domain] {
		if strings.EqualFold(rec.Type, rtype) {
			idx++
		}
	}
	rec := sweb.DNSRecord{Type: rtype, Value: p.Value, Index: sweb.FlexInt(idx), Priority: sweb.FlexInt(p.Priority)}
	switch rtype {
	case "TXT":
		host := p.SubDomain
		if host == "" {
			host = "@"
		}
		rec.Domain = host
	case "MX", "NS":
		rec.Name = p.SubDomain
	default: // A/AAAA/CNAME via editMain
		rec.Name = p.Name
	}
	m.dnsRecords[p.Domain] = append(m.dnsRecords[p.Domain], rec)
	return okSentinel
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
	case "editPtr":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		if m.ptr == nil {
			m.ptr = map[string]string{}
		}
		m.ptr[p["ip"]] = p["ptr"]
		result = 1
	case "getPtr":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		result = m.ptr[p["ip"]]
	case "saveSettings":
		var p struct {
			BillingID string `json:"billingId"`
			Mode      string `json:"mode"`
			Frequency int    `json:"frequency"`
			Time      int    `json:"time"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if m.backupSet == nil {
			m.backupSet = map[string]sweb.BackupSettings{}
		}
		m.backupSet[p.BillingID] = sweb.BackupSettings{
			Mode: p.Mode, Frequency: sweb.FlexInt(p.Frequency), Time: sweb.FlexInt(p.Time),
			NextDataBackup: "2026-07-17",
		}
		result = 1
	case "getSettings":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		result = []sweb.BackupSettings{m.backupSet[p["billingId"]]}
	case "createSubdomain":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		if m.subdomains == nil {
			m.subdomains = map[string][]string{}
		}
		m.subdomains[p["domain"]] = append(m.subdomains[p["domain"]], p["machine"])
		result = 1
	case "removeSubdomain":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		kept := m.subdomains[p["domain"]][:0]
		for _, machine := range m.subdomains[p["domain"]] {
			if machine != p["machine"] {
				kept = append(kept, machine)
			}
		}
		m.subdomains[p["domain"]] = kept
		result = 1
	case "getSubdomains":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		refs := []any{}
		for _, machine := range m.subdomains[p["domain"]] {
			fqdn := machine + "." + p["domain"]
			refs = append(refs, map[string]string{"value": fqdn, "name": fqdn})
		}
		result = refs
	case "setRedirectVh":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		if m.redirect == nil {
			m.redirect = map[string]string{}
		}
		m.redirect[p["domain"]] = p["redirect"]
		result = 1
	case "getRedirectVh":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		result = m.redirect[p["domain"]]
	case "getDomainInfo":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		result = map[string]any{
			"is_our": 1, "registrar": "TEST-REGISTRAR", "expired": "2027-01-01",
			"can_prolong": 1, "autoprolong": "no", "reg_price": 189, "transfer_price": -1,
			"docRoot": "/home/e/example", "siteAlias": "default", "redirectUrl": m.redirect[p["domain"]],
		}
	case "info": // DNS zone (endpoint /domains/dns)
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		recs := m.dnsRecords[p["domain"]]
		if recs == nil {
			recs = []sweb.DNSRecord{}
		}
		result = recs
	case "editMain":
		result = m.editDNS(req.Params, "", true)
	case "editMx":
		result = m.editDNS(req.Params, "MX", 1)
	case "editTxt":
		result = m.editDNS(req.Params, "TXT", true)
	case "editNS":
		result = m.editDNS(req.Params, "NS", true)
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
