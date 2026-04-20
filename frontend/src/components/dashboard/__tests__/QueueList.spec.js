import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import QueueList from '../QueueList.vue'

describe('QueueList', () => {
  it('renders "Queue is empty" when queue is empty', () => {
    const wrapper = mount(QueueList, {
      props: {
        queue: []
      }
    })
    expect(wrapper.text()).toContain('Queue is empty.')
  })

  it('renders a list of songs when queue has items', () => {
    const mockQueue = [
      { id: 1, title: 'Song 1', added_by: 'Alice' },
      { id: 2, title: 'Song 2', added_by: 'Bob' }
    ]
    const wrapper = mount(QueueList, {
      props: {
        queue: mockQueue
      }
    })
    
    const items = wrapper.findAll('.queue-item')
    expect(items).toHaveLength(2)
    expect(items[0].text()).toContain('Song 1')
    expect(items[0].text()).toContain('Added by Alice')
    expect(items[1].text()).toContain('Song 2')
    expect(items[1].text()).toContain('Added by Bob')
  })

  it('shows the correct song count in the header', () => {
    const mockQueue = [
      { id: 1, title: 'Song 1', added_by: 'Alice' }
    ]
    const wrapper = mount(QueueList, {
      props: {
        queue: mockQueue
      }
    })
    
    expect(wrapper.find('.queue-count').text()).toBe('1 songs')
  })
})
