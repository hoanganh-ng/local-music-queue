import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import BaseButton from '../BaseButton.vue'

describe('BaseButton', () => {
  it('renders slot content', () => {
    const wrapper = mount(BaseButton, {
      slots: {
        default: 'Click Me'
      }
    })
    expect(wrapper.text()).toBe('Click Me')
  })

  it('applies the correct variant class', () => {
    const wrapper = mount(BaseButton, {
      props: {
        variant: 'danger'
      }
    })
    expect(wrapper.classes()).toContain('variant-danger')
  })

  it('emits click event when clicked', async () => {
    const wrapper = mount(BaseButton)
    await wrapper.trigger('click')
    expect(wrapper.emitted()).toHaveProperty('click')
  })
})
