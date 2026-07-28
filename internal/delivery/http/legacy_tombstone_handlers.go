package http

// R14c atomic legacy contract retirement. When the server runs in
// authoritative room mode (--room-cutover-authoritative=true) every
// approved legacy global REST method and the global /ws HTTP phase is
// served by the single reusable tombstone below. The tombstone is
// delivery-owned and repository-free: it runs before authentication,
// never resolves a session, never invokes a legacy use case or
// repository, never upgrades /ws, and never exposes a migrated-room
// slug or synthesizes a default room. Route selection is a central
// all-or-nothing registration decision in cmd/server — there is no
// per-route runtime configuration.

import "net/http"

// globalContractRetiredDocumentation is the pointer clients receive to
// the approved retirement plan. It is a repository-relative document
// path, not a live URL, so the tombstone body stays environment-free.
const globalContractRetiredDocumentation = "documents/00-project-management/SPRINTS/022-legacy-global-state-migration-and-contract-retirement-plan.md"

// globalContractTombstoneBody is the exact retirement envelope required
// by Sprint 029. A struct (not a map) keeps the field order of the
// serialized body stable.
type globalContractTombstoneBody struct {
	Error         string `json:"error"`
	Code          string `json:"code"`
	Documentation string `json:"documentation"`
}

// NewGlobalContractTombstone returns the single reusable handler that
// answers every retired legacy global entry point with
//
//	HTTP/1.1 410 Gone
//	Content-Type: application/json
//	Link: </api/rooms>; rel="successor-version"
//
// and the fixed {"error","code","documentation"} JSON body.
func NewGlobalContractTombstone() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `</api/rooms>; rel="successor-version"`)
		writeJSON(w, http.StatusGone, globalContractTombstoneBody{
			Error:         "gone",
			Code:          "global_contract_retired",
			Documentation: globalContractRetiredDocumentation,
		})
	}
}
