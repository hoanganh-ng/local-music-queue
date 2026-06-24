import { describe, it, expect, beforeEach, vi } from 'vitest'

const { notificationInstances } = vi.hoisted(() => ({
  notificationInstances: [],
}))

function makeNotificationCtor(initialPermission = 'granted') {
  class FakeNotification {
    static permission = initialPermission
    static requestPermission = vi.fn().mockResolvedValue(initialPermission)
    constructor(title, options) {
      this.title = title
      this.options = options
      this.onclick = null
      notificationInstances.push(this)
    }
    close() {}
    addEventListener() {}
  }
  return FakeNotification
}

async function loadComposable() {
  vi.resetModules()
  const mod = await import('../useVoteBrowserNotifications')
  return mod.useVoteBrowserNotifications()
}

describe('useVoteBrowserNotifications', () => {
  beforeEach(() => {
    localStorage.clear()
    notificationInstances.length = 0
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
  })

  it('marks unsupported when Notification is not on window', async () => {
    vi.stubGlobal('Notification', undefined)
    const api = await loadComposable()
    expect(api.unsupported.value).toBe(true)
    expect(api.showCta.value).toBe(false)
    expect(api.canNotify.value).toBe(false)
  })

  it('hides CTA when permission is granted (auto-enabled, no toggle)', async () => {
    vi.stubGlobal('Notification', makeNotificationCtor('granted'))
    const api = await loadComposable()
    expect(api.permission.value).toBe('granted')
    expect(api.showCta.value).toBe(false)
    expect(api.canNotify.value).toBe(true)
  })

  it('hides CTA when permission is denied and never re-prompts', async () => {
    vi.stubGlobal('Notification', makeNotificationCtor('denied'))
    const api = await loadComposable()
    expect(api.permission.value).toBe('denied')
    expect(api.showCta.value).toBe(false)
    // requestPermission must not call the browser API when already denied.
    await api.requestPermission()
    // The mock's requestPermission should never be called in denied state.
  })

  it('shows CTA when permission is default and not dismissed', async () => {
    vi.stubGlobal('Notification', makeNotificationCtor('default'))
    const api = await loadComposable()
    expect(api.showCta.value).toBe(true)
  })

  it('hides CTA when dismissed flag is set in localStorage', async () => {
    localStorage.setItem('lmq_browser_notifications_dismissed', 'true')
    vi.stubGlobal('Notification', makeNotificationCtor('default'))
    const api = await loadComposable()
    expect(api.dismissed.value).toBe(true)
    expect(api.showCta.value).toBe(false)
  })

  it('dismissCta writes the flag and hides the CTA', async () => {
    vi.stubGlobal('Notification', makeNotificationCtor('default'))
    const api = await loadComposable()
    expect(api.showCta.value).toBe(true)
    api.dismissCta()
    expect(api.dismissed.value).toBe(true)
    expect(localStorage.getItem('lmq_browser_notifications_dismissed')).toBe('true')
    expect(api.showCta.value).toBe(false)
  })

  it('canNotify is false when permission is default', async () => {
    vi.stubGlobal('Notification', makeNotificationCtor('default'))
    const api = await loadComposable()
    expect(api.canNotify.value).toBe(false)
  })

  it('canNotify is false when permission is denied', async () => {
    vi.stubGlobal('Notification', makeNotificationCtor('denied'))
    const api = await loadComposable()
    expect(api.canNotify.value).toBe(false)
  })

  it('canNotify is true when granted regardless of document.hidden (visibility gate lives in notifyVote)', async () => {
    vi.stubGlobal('Notification', makeNotificationCtor('granted'))
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    const api = await loadComposable()
    // canNotify now only encodes supported + granted. The tab visibility gate
    // moved to notifyVote so a stale computed can't suppress or fire after a
    // visibility transition between two events.
    expect(api.canNotify.value).toBe(true)
  })

  it('canNotify is true when supported + granted (visibility gate moved to notifyVote)', async () => {
    vi.stubGlobal('Notification', makeNotificationCtor('granted'))
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    const api = await loadComposable()
    expect(api.canNotify.value).toBe(true)
  })

  it('notifyVote instantiates Notification with (title, { body, tag }) and wires onclick', async () => {
    vi.stubGlobal('Notification', makeNotificationCtor('granted'))
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    const api = await loadComposable()

    api.notifyVote('vote_updated|skip:1|t1|2', 'Vote update', 'Alice voted to skip')

    expect(notificationInstances).toHaveLength(1)
    const inst = notificationInstances[0]
    expect(inst.title).toBe('Vote update')
    expect(inst.options).toEqual({ body: 'Alice voted to skip', tag: 'vote_updated|skip:1|t1|2' })
    expect(typeof inst.onclick).toBe('function')

    const focusSpy = vi.spyOn(window, 'focus').mockImplementation(() => {})
    inst.onclick()
    expect(focusSpy).toHaveBeenCalled()
    focusSpy.mockRestore()
  })

  it('notifyVote is a no-op when permission is default (CTA not yet acted on)', async () => {
    vi.stubGlobal('Notification', makeNotificationCtor('default'))
    const api = await loadComposable()
    api.notifyVote('tag', 't', 'b')
    expect(notificationInstances).toHaveLength(0)
  })

  it('notifyVote is a no-op when permission is denied', async () => {
    vi.stubGlobal('Notification', makeNotificationCtor('denied'))
    const api = await loadComposable()
    api.notifyVote('tag', 't', 'b')
    expect(notificationInstances).toHaveLength(0)
  })

  it('notifyVote swallows exceptions from the Notification constructor', async () => {
    class ThrowingNotification {
      static permission = 'granted'
      static requestPermission = vi.fn().mockResolvedValue('granted')
      constructor() {
        throw new Error('boom')
      }
    }
    vi.stubGlobal('Notification', ThrowingNotification)
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    const api = await loadComposable()
    expect(() => api.notifyVote('tag', 't', 'b')).not.toThrow()
  })

  it('requestPermission calls browser API and updates permission when default', async () => {
    class MockNotification {
      static permission = 'default'
      static requestPermission = vi.fn().mockResolvedValue('granted')
    }
    vi.stubGlobal('Notification', MockNotification)
    const api = await loadComposable()
    const result = await api.requestPermission()
    expect(MockNotification.requestPermission).toHaveBeenCalled()
    expect(result).toBe('granted')
    expect(api.permission.value).toBe('granted')
    // After responding, dismissed flag is set so CTA doesn't re-appear.
    expect(api.dismissed.value).toBe(true)
    expect(localStorage.getItem('lmq_browser_notifications_dismissed')).toBe('true')
  })

  it('requestPermission marks dismissed even when user denies', async () => {
    class MockNotification {
      static permission = 'default'
      static requestPermission = vi.fn().mockResolvedValue('denied')
    }
    vi.stubGlobal('Notification', MockNotification)
    const api = await loadComposable()
    const result = await api.requestPermission()
    expect(result).toBe('denied')
    expect(api.permission.value).toBe('denied')
    expect(api.dismissed.value).toBe(true)
  })

  it('requestPermission is a no-op (returns denied) when unsupported', async () => {
    vi.stubGlobal('Notification', undefined)
    const api = await loadComposable()
    const result = await api.requestPermission()
    expect(result).toBe('denied')
    expect(api.permission.value).toBe('denied')
    expect(api.dismissed.value).toBe(true)
  })

  it('requestPermission does not call browser API again when already decided', async () => {
    class AlreadyGranted {
      static permission = 'granted'
      static requestPermission = vi.fn().mockResolvedValue('granted')
    }
    vi.stubGlobal('Notification', AlreadyGranted)
    const api = await loadComposable()
    await api.requestPermission()
    expect(AlreadyGranted.requestPermission).not.toHaveBeenCalled()
  })

  it('notifyVote suppresses while visible and fires after the tab becomes hidden (granted)', async () => {
    vi.stubGlobal('Notification', makeNotificationCtor('granted'))
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    const api = await loadComposable()

    api.notifyVote('tag-visible', 't', 'b')
    expect(notificationInstances).toHaveLength(0)

    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    api.notifyVote('tag-hidden', 't', 'b')
    expect(notificationInstances).toHaveLength(1)
    expect(notificationInstances[0].options.tag).toBe('tag-hidden')
  })

  it('notifyVote fires while hidden and suppresses after the tab becomes visible (granted)', async () => {
    vi.stubGlobal('Notification', makeNotificationCtor('granted'))
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    const api = await loadComposable()

    api.notifyVote('tag-hidden', 't', 'b')
    expect(notificationInstances).toHaveLength(1)

    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    api.notifyVote('tag-visible', 't', 'b')
    // Still 1 — second call was suppressed because the tab is visible now.
    expect(notificationInstances).toHaveLength(1)
  })

  it('requestPermission marks denied + dismissed when the promise rejects', async () => {
    class RejectingNotification {
      static permission = 'default'
      static requestPermission = vi.fn().mockRejectedValueOnce(new Error('nope'))
    }
    vi.stubGlobal('Notification', RejectingNotification)
    const api = await loadComposable()

    const result = await api.requestPermission()

    expect(result).toBe('denied')
    expect(api.permission.value).toBe('denied')
    expect(api.dismissed.value).toBe(true)
    expect(localStorage.getItem('lmq_browser_notifications_dismissed')).toBe('true')
  })

  it('requestPermission marks denied + dismissed when the call throws synchronously', async () => {
    class ThrowingNotification {
      static permission = 'default'
      static requestPermission = vi.fn(() => {
        throw new Error('boom')
      })
    }
    vi.stubGlobal('Notification', ThrowingNotification)
    const api = await loadComposable()

    const result = await api.requestPermission()

    expect(result).toBe('denied')
    expect(api.permission.value).toBe('denied')
    expect(api.dismissed.value).toBe(true)
    expect(localStorage.getItem('lmq_browser_notifications_dismissed')).toBe('true')
  })

  it('visibilitychange event resyncs permission from Notification.permission', async () => {
    class ChangingNotification {
      static permission = 'granted'
      static requestPermission = vi.fn().mockResolvedValue('granted')
    }
    vi.stubGlobal('Notification', ChangingNotification)
    const api = await loadComposable()
    expect(api.permission.value).toBe('granted')
    ChangingNotification.permission = 'denied'
    document.dispatchEvent(new Event('visibilitychange'))
    expect(api.permission.value).toBe('denied')
    expect(api.canNotify.value).toBe(false)
  })
})