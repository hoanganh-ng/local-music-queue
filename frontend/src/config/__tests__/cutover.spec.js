import { describe, it, expect } from 'vitest'
import {
  parseRoomCutoverAuthoritative,
  roomCutoverAuthoritative,
  authenticatedLandingRouteName,
} from '../cutover'

// R14d: the single frontend owner of VITE_ROOM_CUTOVER_AUTHORITATIVE.
// The parse rules are tested directly against raw values, so this file
// is deterministic no matter which env value the suite is baked with.

describe('parseRoomCutoverAuthoritative', () => {
  it('missing value (undefined) selects false', () => {
    expect(parseRoomCutoverAuthoritative(undefined)).toBe(false)
  })

  it('null selects false', () => {
    expect(parseRoomCutoverAuthoritative(null)).toBe(false)
  })

  it('empty string selects false', () => {
    expect(parseRoomCutoverAuthoritative('')).toBe(false)
  })

  it('"false" selects false', () => {
    expect(parseRoomCutoverAuthoritative('false')).toBe(false)
  })

  it('"true" selects true', () => {
    expect(parseRoomCutoverAuthoritative('true')).toBe(true)
  })

  it('any other non-empty value fails explicitly', () => {
    for (const invalid of ['TRUE', 'False', '1', '0', 'yes', 'no', ' true', 'true ']) {
      expect(() => parseRoomCutoverAuthoritative(invalid)).toThrow(
        /VITE_ROOM_CUTOVER_AUTHORITATIVE/
      )
    }
  })

  it('the thrown error never echoes the invalid raw value (no env leakage)', () => {
    try {
      parseRoomCutoverAuthoritative('sekret-value')
      expect.unreachable('should have thrown')
    } catch (e) {
      expect(e.message).not.toContain('sekret-value')
    }
  })
})

describe('baked build-time constant', () => {
  it('matches the env value the suite was baked with', () => {
    expect(roomCutoverAuthoritative).toBe(
      parseRoomCutoverAuthoritative(import.meta.env.VITE_ROOM_CUTOVER_AUTHORITATIVE)
    )
  })

  it('authenticatedLandingRouteName follows the baked mode (false → Dashboard, true → RoomEntry)', () => {
    expect(authenticatedLandingRouteName()).toBe(
      roomCutoverAuthoritative ? 'RoomEntry' : 'Dashboard'
    )
  })
})
