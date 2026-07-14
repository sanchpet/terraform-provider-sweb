package provider

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/sanchpet/sweb-go-sdk/flex"
	"github.com/sanchpet/sweb-go-sdk/monitoring/contacts"
)

// mockMonitoringContact is the path-scoped JSON-RPC handler for the
// /monitoring/contacts endpoint (registered from init below). It backs the
// sweb_monitoring_contact acceptance test with an in-memory contact list in
// m.contacts, keyed by the numeric id the add methods hand back. The mock lock is
// already held by the dispatcher — this must not lock.
func mockMonitoringContact(m *mockSweb, path string, req rpcReq) (any, bool) {
	if !strings.Contains(path, "/monitoring/contacts") {
		return nil, false
	}
	switch req.Method {
	case "addEmail":
		var p struct{ Email, Name string }
		_ = json.Unmarshal(req.Params, &p)
		return m.addContact(contacts.ContactEmail, p.Email, p.Name), true
	case "addPhone":
		var p struct{ Phone, Name string }
		_ = json.Unmarshal(req.Params, &p)
		return m.addContact(contacts.ContactPhone, p.Phone, p.Name), true
	case "addTelegram":
		var p struct{ Name string }
		_ = json.Unmarshal(req.Params, &p)
		return m.addContact(contacts.ContactTelegram, "", p.Name), true
	case "index":
		// The SDK decodes ContactList: {list:[...], filterInfo:{...}}.
		list := m.contacts
		if list == nil {
			list = []contacts.Contact{}
		}
		return map[string]any{
			"list":       list,
			"filterInfo": map[string]any{"page": 1, "perPage": len(list), "totalCount": len(list)},
		}, true
	case "editEmail":
		var p struct{ ContactID, Email, Name string }
		_ = json.Unmarshal(req.Params, &p)
		return m.editContact(p.ContactID, p.Email, p.Name, false), true
	case "editPhone":
		var p struct{ ContactID, Phone, Name string }
		_ = json.Unmarshal(req.Params, &p)
		return m.editContact(p.ContactID, p.Phone, p.Name, false), true
	case "editContact":
		var p struct{ ContactID, Value, Name string }
		_ = json.Unmarshal(req.Params, &p)
		return m.editContact(p.ContactID, p.Value, p.Name, false), true
	case "editTelegram":
		var p struct{ ContactID, Name string }
		_ = json.Unmarshal(req.Params, &p)
		return m.editContact(p.ContactID, "", p.Name, true), true
	case "deleteContact":
		var p struct{ ContactID string }
		_ = json.Unmarshal(req.Params, &p)
		return m.deleteContact(p.ContactID), true
	}
	return nil, false
}

// addContact appends a contact with a fresh id and returns that id as the add
// methods' result (the SDK decodes it through flex.Int).
func (m *mockSweb) addContact(ctype, value, name string) int {
	m.seq2++
	m.contacts = append(m.contacts, contacts.Contact{
		ID: flex.Int(m.seq2), Type: ctype, Value: value, Name: name, Verified: false,
	})
	return m.seq2
}

// editContact mutates the matching contact in place, returning the integer 1
// sentinel the edit family expects. nameOnly leaves value untouched (Telegram).
func (m *mockSweb) editContact(id, value, name string, nameOnly bool) int {
	for i := range m.contacts {
		if strconv.FormatInt(int64(m.contacts[i].ID), 10) == id {
			m.contacts[i].Name = name
			if !nameOnly {
				m.contacts[i].Value = value
			}
		}
	}
	return 1
}

// deleteContact removes the matching contact, returning the integer 1 sentinel.
func (m *mockSweb) deleteContact(id string) int {
	kept := m.contacts[:0]
	for _, c := range m.contacts {
		if strconv.FormatInt(int64(c.ID), 10) != id {
			kept = append(kept, c)
		}
	}
	m.contacts = kept
	return 1
}

func init() {
	cloudMockHandlers = append(cloudMockHandlers, mockMonitoringContact)
}
