import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'

const DEBOUNCE_MS = 200

export function SearchInput() {
  const [query, setQuery] = useState('')
  const [suggestions, setSuggestions] = useState<string[]>([])
  const [showDropdown, setShowDropdown] = useState(false)
  const [activeIndex, setActiveIndex] = useState(-1)
  const navigate = useNavigate()
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const latestQueryRef = useRef('')

  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [])

  const goToSearch = (q: string) => {
    if (!q.trim()) return
    setShowDropdown(false)
    navigate(`/search?q=${encodeURIComponent(q.trim())}`)
  }

  const handleChange = (value: string) => {
    setQuery(value)
    setActiveIndex(-1)
    latestQueryRef.current = value

    if (debounceRef.current) clearTimeout(debounceRef.current)

    if (!value.trim()) {
      setSuggestions([])
      setShowDropdown(false)
      return
    }

    debounceRef.current = setTimeout(() => {
      fetch(`/api/v1/search/autocomplete?q=${encodeURIComponent(value.trim())}&limit=8`)
        .then((res) => (res.ok ? res.json() : { suggestions: [] }))
        .then((data) => {
          if (latestQueryRef.current !== value) return
          setSuggestions(data.suggestions || [])
          setShowDropdown(true)
        })
        .catch(() => {})
    }, DEBOUNCE_MS)
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (activeIndex >= 0 && suggestions[activeIndex]) {
      goToSearch(suggestions[activeIndex])
    } else {
      goToSearch(query)
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (!showDropdown || suggestions.length === 0) return

    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActiveIndex((i) => (i + 1) % suggestions.length)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActiveIndex((i) => (i <= 0 ? suggestions.length - 1 : i - 1))
    } else if (e.key === 'Escape') {
      setShowDropdown(false)
      setActiveIndex(-1)
    }
  }

  return (
    <form onSubmit={handleSubmit} className="relative">
      <input
        type="text"
        value={query}
        onChange={(e) => handleChange(e.target.value)}
        onKeyDown={handleKeyDown}
        onFocus={() => suggestions.length > 0 && setShowDropdown(true)}
        onBlur={() => setTimeout(() => setShowDropdown(false), 150)}
        placeholder="Search artifacts, packages, images..."
        autoComplete="off"
        className="w-full bg-gray-800/50 border border-gray-700 rounded-lg py-2.5 pl-4 pr-10 text-gray-100 placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-transparent transition-all shadow-sm"
      />
      <button
        type="submit"
        className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 hover:text-blue-400 transition-colors"
      >
        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
        </svg>
      </button>

      {showDropdown && suggestions.length > 0 && (
        <ul className="absolute z-20 mt-1 w-full bg-gray-800 border border-gray-700 rounded-lg shadow-lg overflow-hidden">
          {suggestions.map((s, idx) => (
            <li key={s}>
              <button
                type="button"
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => goToSearch(s)}
                onMouseEnter={() => setActiveIndex(idx)}
                className={`w-full text-left px-4 py-2 text-sm transition-colors ${
                  idx === activeIndex ? 'bg-gray-700 text-blue-300' : 'text-gray-200 hover:bg-gray-700'
                }`}
              >
                {s}
              </button>
            </li>
          ))}
        </ul>
      )}
    </form>
  )
}
