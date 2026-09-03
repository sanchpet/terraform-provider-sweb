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
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/sanchpet/sweb-go-sdk/backup"
	"github.com/sanchpet/sweb-go-sdk/balancer"
	"github.com/sanchpet/sweb-go-sdk/dbaas"
	"github.com/sanchpet/sweb-go-sdk/dns"
	"github.com/sanchpet/sweb-go-sdk/flex"
	swebip "github.com/sanchpet/sweb-go-sdk/ip"
	"github.com/sanchpet/sweb-go-sdk/monitoring/checks"
	"github.com/sanchpet/sweb-go-sdk/monitoring/contacts"
	"github.com/sanchpet/sweb-go-sdk/sites"
	"github.com/sanchpet/sweb-go-sdk/vh/cron"
	"github.com/sanchpet/sweb-go-sdk/vh/hosting"
	"github.com/sanchpet/sweb-go-sdk/vh/mail"
	"github.com/sanchpet/sweb-go-sdk/vh/ssl"
	"github.com/sanchpet/sweb-go-sdk/vps"
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
	mu              sync.Mutex
	nodes           []vps.VPS
	seq             int
	localAttached   map[string]bool             // billingId → attached to the local network
	ptr             map[string]string           // ip → PTR record
	backupSet       map[string]backup.Settings  // billingId → auto-backup schedule
	subdomains      map[string][]string         // domain → subdomain machine labels
	redirect        map[string]string           // domain → redirect URL
	dnsRecords      map[string][]dns.Record     // domain → DNS zone records
	dnsVersion      map[string]int              // domain → zone mutation counter
	dnsReadAt       map[string]int              // domain → zone version the last read served
	dnsStaleDeletes []string                    // deletes issued against a zone that moved under them
	dnsReadDelay    time.Duration               // test knob: latency on the zone read, to widen the race window
	mailboxes       map[string][]mail.Mailbox   // domain → mailboxes (Mbox is the full address)
	sites           []sites.Site                // account-level websites (identity = DocRoot)
	cronTasks       []cron.Task                 // account-level crontab entries (identity = Task line)
	databases       []hosting.Database          // account-level databases (identity = Name)
	certs           []ssl.Certificate           // account-level SSL certificates (identity = Domain)
	balancers       []balancer.Balancer         // cloud load balancers (identity = BillingID)
	dbaas           []dbaas.Instance            // managed-database clusters (identity = BillingID)
	checks          []checks.Check              // monitoring checks (identity = numeric id)
	contacts        []contacts.Contact          // monitoring contacts (identity = numeric id)
	seq2            int                         // secondary sequence for cloud-tier ids (avoids racing seq)
	publicIPs       map[string][]swebip.Address // billingId → additional public IPs (/vps/ip)
	ipSeq           int                         // sequence for the addresses the mock assigns
	ipBusyOrders    int                         // test knob: refuse this many orders with the busy -32500
	ipRemoveLocked  int                         // test knob: refuse this many releases with the 24h-lock -32500
}

// cloudMockHandlers lets each cloud-tier resource's acceptance test register a
// path-scoped JSON-RPC handler in its own file, instead of editing the shared
// method switch (whose method names — index/create/edit/remove/getAvailableConfig
// — collide across endpoints). A handler inspects (path, method) and returns
// (result, true) when it owns the call, or (nil, false) to defer to the next
// handler / the main switch. Registered from each test file's init().
var cloudMockHandlers []func(m *mockSweb, path string, req rpcReq) (any, bool)

// dnsGroup maps a record type to the index space the API addresses it in. The
// editMain types (A/AAAA/CNAME) share one sequence — a live zone numbers a CNAME
// 2 after two A records — while each dedicated method (MX/TXT/NS/SRV) counts from
// zero on its own.
func dnsGroup(recType string) string {
	switch t := strings.ToUpper(recType); t {
	case "MX", "TXT", "NS", "SRV":
		return t
	default:
		return "main"
	}
}

// dnsZone returns a domain's records with each Index set to the record's current
// position in its index group. The API numbers positionally, so removing a record
// renumbers every record after it — the property that makes an index derived
// before someone else's delete point at the wrong record.
func (m *mockSweb) dnsZone(domain string) []dns.Record {
	recs := append([]dns.Record(nil), m.dnsRecords[domain]...)
	pos := map[string]int{}
	for i := range recs {
		g := dnsGroup(recs[i].Type)
		recs[i].Index = flex.Int(pos[g])
		pos[g]++
	}
	return recs
}

// editDNS applies an add/del to the mock zone, mirroring the real API's
// positional index addressing (host in Domain for TXT, Name otherwise).
// okSentinel is the method's success value (integer 1 for editMx, boolean true
// for the rest).
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
		Service   string `json:"service"`
		Protocol  string `json:"protocol"`
		Target    string `json:"target"`
		Port      int    `json:"port"`
		Weight    int    `json:"weight"`
		TTL       int    `json:"ttl"`
	}
	_ = json.Unmarshal(raw, &p)
	if m.dnsRecords == nil {
		m.dnsRecords = map[string][]dns.Record{}
	}
	if p.Action == "del" {
		// A delete addressed by an index derived before an earlier delete shifted
		// the zone removes the wrong record. The mock can't see the caller's
		// intent, but it can see that the zone changed since the last read it
		// served, which is exactly that hazard — record it for the test.
		if m.dnsReadAt[p.Domain] < m.dnsVersion[p.Domain] {
			m.dnsStaleDeletes = append(m.dnsStaleDeletes,
				fmt.Sprintf("%s %s index %d", p.Domain, dnsGroup(p.Type), p.Index))
		}
		group, pos := dnsGroup(p.Type), 0
		kept := make([]dns.Record, 0, len(m.dnsRecords[p.Domain]))
		for _, rec := range m.dnsRecords[p.Domain] {
			if dnsGroup(rec.Type) != group {
				kept = append(kept, rec)
				continue
			}
			if pos != p.Index {
				kept = append(kept, rec)
			}
			pos++
		}
		m.dnsRecords[p.Domain] = kept
		m.dnsVersion[p.Domain]++
		return okSentinel
	}
	rtype := fixedType
	if rtype == "" {
		rtype = strings.ToUpper(p.Type)
	}
	// Index is not stored: it is a position, derived on read by dnsZone.
	rec := dns.Record{Type: rtype, Value: p.Value, Priority: flex.Int(p.Priority)}
	switch rtype {
	case "TXT":
		host := p.SubDomain
		if host == "" {
			host = "@"
		}
		rec.Domain = host
	case "MX", "NS":
		rec.Name = p.SubDomain
	case "SRV":
		rec.Name = p.SubDomain
		rec.Service = p.Service
		rec.Protocol = p.Protocol
		rec.Target = p.Target
		rec.Port = flex.Int(p.Port)
		rec.Weight = flex.Int(p.Weight)
		rec.TTL = flex.Int(p.TTL)
	default: // A/AAAA/CNAME via editMain
		rec.Name = p.Name
	}
	m.dnsRecords[p.Domain] = append(m.dnsRecords[p.Domain], rec)
	m.dnsVersion[p.Domain]++
	return okSentinel
}

// mutateMailbox applies mut to the mailbox on domain whose local part matches
// mbox, returning the sentinel 1 the mail setters expect.
func (m *mockSweb) mutateMailbox(domain, mbox string, mut func(*mail.Mailbox)) int {
	for i := range m.mailboxes[domain] {
		if mailboxLocalPart(m.mailboxes[domain][i].Mbox) == mbox {
			mut(&m.mailboxes[domain][i])
		}
	}
	return 1
}

// cronSchedule decodes the named-key schedule params addTask/editTask send.
type cronSchedule struct {
	Minute  int    `json:"minute"`
	Hour    int    `json:"hour"`
	Day     int    `json:"day"`
	Month   int    `json:"month"`
	Weekday int    `json:"weekday"`
	Command string `json:"command"`
}

// cronTaskFrom builds a live cron.Task from a schedule, mirroring the API: the
// Task field is the raw crontab line (the removeTask key).
func cronTaskFrom(p cronSchedule) cron.Task {
	return cron.Task{
		Minute: flex.Int(p.Minute), Hour: flex.Int(p.Hour), Day: flex.Int(p.Day),
		Month: flex.Int(p.Month), Weekday: flex.Int(p.Weekday), Command: p.Command,
		Task: fmt.Sprintf("%d %d %d %d %d %s", p.Minute, p.Hour, p.Day, p.Month, p.Weekday, p.Command),
	}
}

type rpcReq struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// rpcError makes a path-scoped handler answer with a JSON-RPC error envelope
// instead of a result — the only way to exercise the provider's API-error paths
// (the 24h IP-release lock, the "another operation is running" refusal).
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func newMockSweb() *httptest.Server {
	srv, _ := newMockSwebDelayedDNS(0)
	return srv
}

// newMockSwebDelayedDNS is newMockSweb with an artificial latency on the DNS zone
// read, plus a handle on the mock state for tests that must assert what the
// server ended up holding rather than what Terraform believes.
func newMockSwebDelayedDNS(dnsReadDelay time.Duration) (*httptest.Server, *mockSweb) {
	m := &mockSweb{
		dnsRecords:   map[string][]dns.Record{},
		dnsVersion:   map[string]int{},
		dnsReadAt:    map[string]int{},
		dnsReadDelay: dnsReadDelay,
	}
	return httptest.NewServer(http.HandlerFunc(m.handle)), m
}

func (m *mockSweb) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req rpcReq
	_ = json.Unmarshal(body, &req)

	// Held before the lock so concurrent readers overlap instead of queueing:
	// the point is to serve them all the same pre-mutation zone.
	if req.Method == "info" && m.dnsReadDelay > 0 && strings.HasSuffix(r.URL.Path, "/domains/dns") {
		time.Sleep(m.dnsReadDelay)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Cloud-tier resources register path-scoped handlers (balancer/dbaas/monitoring)
	// to avoid colliding with the shared method switch below.
	for _, h := range cloudMockHandlers {
		res, ok := h(m, r.URL.Path, req)
		if !ok {
			continue
		}
		key := "result"
		if _, isErr := res.(rpcError); isErr {
			key = "error"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{key: res})
		return
	}

	var result any
	switch req.Method {
	case "getToken":
		result = "test-token"
	case "getConstructorPlanId":
		result = 379
	case "create":
		var p vps.CreateRequest
		_ = json.Unmarshal(req.Params, &p)
		m.seq++
		m.nodes = append(m.nodes, vps.VPS{
			BillingID:    fmt.Sprintf("petrovpet2_vps_%d", m.seq),
			UID:          fmt.Sprintf("uid-%d", m.seq),
			Name:         p.Alias,
			IP:           "203.0.113.50",
			IsRunning:    1,
			CPU:          2,
			RAM:          6,
			Disk:         "15 ГБ", // index reports disk as a localized string, not diskGb
			PlanID:       flex.Int(p.VPSPlanID),
			OSDistrID:    flex.Int(p.DistributiveID),
			DatacenterID: strconv.Itoa(p.Datacenter),
		})
		result = map[string]bool{"ok": true}
	case "index":
		switch {
		case strings.HasSuffix(r.URL.Path, "/sites"):
			result = m.sites // website inventory (endpoint /sites)
		case strings.HasSuffix(r.URL.Path, "/vh/ssl"):
			result = map[string]any{"list": m.certs, "filterInfo": map[string]any{"totalCount": len(m.certs)}}
		default:
			result = m.nodes
		}
	case "installLetsEncrypt": // /vh/ssl: issue a free Let's Encrypt certificate
		var p map[string]any
		_ = json.Unmarshal(req.Params, &p)
		m.seq++
		m.certs = append(m.certs, ssl.Certificate{
			ID: flex.Int(m.seq), Status: "active", Domain: fmt.Sprint(p["domain"]),
			Name: "Let's Encrypt", ValidTo: "2027-01-01", Autoprolong: false,
		})
		result = 1
	case "editAutoprolong": // /vh/ssl: toggle auto-prolongation
		var p struct {
			CertificateID int  `json:"certificateId"`
			Autoprolong   bool `json:"autoprolong"`
		}
		_ = json.Unmarshal(req.Params, &p)
		for i := range m.certs {
			if int(m.certs[i].ID) == p.CertificateID {
				m.certs[i].Autoprolong = p.Autoprolong
			}
		}
		result = 1
	case "removeCertificate": // /vh/ssl: delete a certificate
		var p struct {
			CertificateID int `json:"certificateId"`
		}
		_ = json.Unmarshal(req.Params, &p)
		kept := m.certs[:0]
		for _, c := range m.certs {
			if int(c.ID) != p.CertificateID {
				kept = append(kept, c)
			}
		}
		m.certs = kept
		result = 1
	case "add": // /sites: create website
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		m.seq++
		m.sites = append(m.sites, sites.Site{
			ID: flex.Int(m.seq), DocRoot: p["docRoot"], DocRootFull: "/home/" + p["docRoot"], Alias: p["alias"],
		})
		result = 1
	case "edit": // /sites: rename / move website
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		for i := range m.sites {
			if m.sites[i].DocRoot == p["docRoot"] {
				m.sites[i].Alias = p["alias"]
				if p["docRootNew"] != "" {
					m.sites[i].DocRoot = p["docRootNew"]
					m.sites[i].DocRootFull = "/home/" + p["docRootNew"]
				}
			}
		}
		result = 1
	case "del": // /sites: delete website
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		kept := m.sites[:0]
		for _, s := range m.sites {
			if s.DocRoot != p["docRoot"] {
				kept = append(kept, s)
			}
		}
		m.sites = kept
		result = 1
	case "getTasks": // /vh/cron: list crontab entries
		result = m.cronTasks
	case "addTask": // /vh/cron: add crontab entry
		var p cronSchedule
		_ = json.Unmarshal(req.Params, &p)
		m.cronTasks = append(m.cronTasks, cronTaskFrom(p))
		result = 1
	case "removeTask": // /vh/cron: remove crontab entry
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		kept := m.cronTasks[:0]
		for _, t := range m.cronTasks {
			if t.Task != p["task"] {
				kept = append(kept, t)
			}
		}
		m.cronTasks = kept
		result = 1
	case "databaseGetList": // /vh/hosting: list databases
		dbs := m.databases
		if dbs == nil {
			dbs = []hosting.Database{}
		}
		result = map[string]any{"list": dbs, "params": map[string]any{"server": "mysql-1"}}
	case "databaseMysqlCreate": // /vh/hosting: create MySQL database
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		version := p["dbVersion"]
		if version == "" {
			version = "8.0"
		}
		m.databases = append(m.databases, hosting.Database{
			Type: "mysql", Name: p["dbName"], Login: p["dbName"], Comment: p["dbComment"],
			Version: version, Charset: "utf8mb4",
		})
		result = 1
	case "databaseMysqlDelete": // /vh/hosting: delete MySQL database
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		kept := m.databases[:0]
		for _, db := range m.databases {
			if db.Name != p["dbName"] {
				kept = append(kept, db)
			}
		}
		m.databases = kept
		result = 1
	case "databaseMysqlChangePass": // /vh/hosting: change password (not stored)
		result = 1
	case "databaseEditComment": // /vh/hosting: edit database comment
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		for i := range m.databases {
			if m.databases[i].Name == p["dbName"] {
				m.databases[i].Comment = p["dbComment"]
			}
		}
		result = 1
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
				m.nodes[i].PlanID = flex.Int(p.PlanID)
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
			m.backupSet = map[string]backup.Settings{}
		}
		m.backupSet[p.BillingID] = backup.Settings{
			Mode: p.Mode, Frequency: flex.Int(p.Frequency), Time: flex.Int(p.Time),
			NextDataBackup: "2026-07-17",
		}
		result = 1
	case "getSettings":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		result = []backup.Settings{m.backupSet[p["billingId"]]}
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
		m.dnsReadAt[p["domain"]] = m.dnsVersion[p["domain"]]
		recs := m.dnsZone(p["domain"])
		if recs == nil {
			recs = []dns.Record{}
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
	case "editSrv":
		result = m.editDNS(req.Params, "SRV", true)
	case "createMbox":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		if m.mailboxes == nil {
			m.mailboxes = map[string][]mail.Mailbox{}
		}
		m.mailboxes[p["domain"]] = append(m.mailboxes[p["domain"]], mail.Mailbox{
			Mbox: p["mbox"] + "@" + p["domain"], Quota: 1024, Comment: p["comment"],
		})
		// createMbox answers a rich NewMailbox object; only the shape is decoded.
		result = map[string]any{"login": p["mbox"] + "@" + p["domain"], "password": p["password"]}
	case "dropMbox":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		kept := m.mailboxes[p["domain"]][:0]
		for _, mb := range m.mailboxes[p["domain"]] {
			if mailboxLocalPart(mb.Mbox) != p["mbox"] {
				kept = append(kept, mb)
			}
		}
		m.mailboxes[p["domain"]] = kept
		result = 1
	case "getMailboxesList":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		boxes := m.mailboxes[p["domain"]]
		if boxes == nil {
			boxes = []mail.Mailbox{}
		}
		result = map[string]any{"list": boxes, "filterInfo": map[string]any{"totalCount": len(boxes)}}
	case "changeMailboxPassword":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		result = m.mutateMailbox(p["domain"], p["mbox"], func(*mail.Mailbox) {}) // password not stored
	case "updateComment":
		var p map[string]string
		_ = json.Unmarshal(req.Params, &p)
		result = m.mutateMailbox(p["domain"], p["mbox"], func(mb *mail.Mailbox) { mb.Comment = p["comment"] })
	case "updateAntispamState":
		var p struct {
			Domain string `json:"domain"`
			Mbox   string `json:"mbox"`
			Value  int    `json:"value"`
		}
		_ = json.Unmarshal(req.Params, &p)
		result = m.mutateMailbox(p.Domain, p.Mbox, func(mb *mail.Mailbox) { mb.Antispam = flex.Int(p.Value) })
	case "changeMailboxSpf":
		var p struct {
			Domain string `json:"domain"`
			Mbox   string `json:"mbox"`
			TurnOn bool   `json:"turnOn"`
		}
		_ = json.Unmarshal(req.Params, &p)
		result = m.mutateMailbox(p.Domain, p.Mbox, func(mb *mail.Mailbox) {
			if p.TurnOn {
				mb.SPF = 1
			} else {
				mb.SPF = 0
			}
		})
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
