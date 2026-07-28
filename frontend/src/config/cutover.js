// R14d: single frontend owner of the approved Vite build-time cutover gate.
//
// VITE_ROOM_CUTOVER_AUTHORITATIVE is baked into the bundle at build time
// (import.meta.env). Two artifacts are built from the same source:
//
//   - false: the pre-cutover / rollback artifact. Dashboard, the global
//     store slices, global REST usage, and the global /ws client keep
//     their existing behavior.
//   - true:  the post-R14c artifact. RoomEntry is the authenticated
//     landing surface, the Dashboard route is absent, legacy global
//     runtime state is retired at bootstrap, and the global /ws client
//     cannot connect or reconnect.
//
// This module is the ONLY place the setting is parsed. No other module
// may read import.meta.env.VITE_ROOM_CUTOVER_AUTHORITATIVE directly,
// and no runtime configuration request, local-storage switch, query
// parameter, cookie, or feature-flag service is permitted (R14a §
// Phase C contract: the SPA is built twice; there is NO runtime flag).

// parseRoomCutoverAuthoritative maps the raw build-time string to the
// baked boolean. Missing/empty and "false" select false; "true" selects
// true; any other non-empty value fails explicitly so a typo in a build
// pipeline can never silently produce the wrong artifact.
export function parseRoomCutoverAuthoritative(raw) {
  if (raw === undefined || raw === null || raw === '') return false
  if (raw === 'false') return false
  if (raw === 'true') return true
  throw new Error(
    'VITE_ROOM_CUTOVER_AUTHORITATIVE must be "true", "false", or unset; got an invalid value'
  )
}

// roomCutoverAuthoritative is the baked build-time constant. The SPA
// checks it once at module-evaluation time; it never changes at runtime.
export const roomCutoverAuthoritative = parseRoomCutoverAuthoritative(
  import.meta.env.VITE_ROOM_CUTOVER_AUTHORITATIVE
)

// authenticatedLandingRouteName is the single shared cutover-aware
// landing decision: where an authenticated user lands when they are
// navigated away from /auth (router guard) or complete a successful
// Google login (AuthView). Callers MUST NOT duplicate this conditional.
export function authenticatedLandingRouteName() {
  return roomCutoverAuthoritative ? 'RoomEntry' : 'Dashboard'
}
