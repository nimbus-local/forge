'use client'

import { useState, useEffect } from 'react'

interface Item {
  itemId: string
  text: string
  done: boolean
  createdAt: string
}

export default function Home() {
  const [items, setItems] = useState<Item[]>([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch('/api/items')
      .then(r => r.json())
      .then((data: Item[]) => {
        setItems(data)
        setLoading(false)
      })
  }, [])

  async function addItem(e: React.FormEvent) {
    e.preventDefault()
    const text = input.trim()
    if (!text) return

    setInput('')
    const res = await fetch('/api/items', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text }),
    })
    const item: Item = await res.json()
    setItems(prev => [item, ...prev])
  }

  async function toggleItem(id: string, done: boolean) {
    const res = await fetch(`/api/items/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ done: !done }),
    })
    const updated: Item = await res.json()
    setItems(prev => prev.map(i => (i.itemId === id ? updated : i)))
  }

  async function deleteItem(id: string) {
    await fetch(`/api/items/${id}`, { method: 'DELETE' })
    setItems(prev => prev.filter(i => i.itemId !== id))
  }

  if (loading) return <div className="loading">Loading...</div>

  return (
    <main className="container">
      <h1>My Checklist</h1>

      <form onSubmit={addItem} className="add-form">
        <input
          value={input}
          onChange={e => setInput(e.target.value)}
          placeholder="Add an item..."
          className="add-input"
          autoFocus
        />
        <button type="submit" className="add-btn">Add</button>
      </form>

      <ul className="item-list">
        {items.map(item => (
          <li key={item.itemId} className={`item${item.done ? ' done' : ''}`}>
            <input
              type="checkbox"
              checked={item.done}
              onChange={() => toggleItem(item.itemId, item.done)}
            />
            <span>{item.text}</span>
            <button
              onClick={() => deleteItem(item.itemId)}
              className="delete-btn"
              aria-label="Delete"
            >
              ×
            </button>
          </li>
        ))}
      </ul>

      {items.length === 0 && (
        <p className="empty">No items yet — add something above.</p>
      )}

      <p className="hint">
        Your list is stored anonymously. Clearing cookies will start a fresh list.
      </p>
    </main>
  )
}
