package provider

var dnsZoneLocks keyedMutex

// lockDNSZone serializes mutating DNS operations on one zone and returns the
// unlock function, so a caller writes `defer lockDNSZone(domain)()`.
//
// SpaceWeb addresses a DNS record by its position in the zone, and removing one
// renumbers every record after it. The resources therefore re-read the zone and
// re-derive the index immediately before each write — sound in isolation, unsound
// under Terraform's default -parallelism=10: several deletes in one apply each
// derived an index from the same pre-mutation zone, and every delete after the
// first removed whichever record had shifted into that position. That destroys
// records the plan never mentioned, and Terraform reports success. Holding this
// lock across read → derive index → write closes the window.
//
// Same reasoning as createMu in vps_resource.go: the API is a single writer.
// Keyed per zone, so unrelated zones in one apply still run in parallel. The
// scope is one provider process, which covers a Terraform run; two runs against
// the same zone at once remain unsafe, and the API offers no way to make them
// safe (no conditional write, no stable record id).
func lockDNSZone(domain string) func() { return dnsZoneLocks.lock(domain) }
