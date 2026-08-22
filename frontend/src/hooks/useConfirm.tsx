import { useCallback, useRef, useState } from 'react'

interface ConfirmOptions {
  title?: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  danger?: boolean
}

// In-app replacement for window.confirm() — native browser dialogs can't be
// styled, block the whole tab (including other async work), and are
// suppressible by the user, so destructive actions render this instead.
export function useConfirm() {
  const [options, setOptions] = useState<ConfirmOptions | null>(null)
  const resolver = useRef<(value: boolean) => void>()

  const confirm = useCallback((opts: ConfirmOptions | string) => {
    setOptions(typeof opts === 'string' ? { message: opts } : opts)
    return new Promise<boolean>((resolve) => {
      resolver.current = resolve
    })
  }, [])

  const resolve = (result: boolean) => {
    setOptions(null)
    resolver.current?.(result)
  }

  const ConfirmDialog = !options ? null : (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      onClick={() => resolve(false)}
    >
      <div className="card max-w-sm w-full" onClick={(e) => e.stopPropagation()} role="alertdialog" aria-modal="true">
        {options.title && <h3 className="text-lg font-semibold text-gray-100 mb-2">{options.title}</h3>}
        <p className="text-sm text-gray-300 mb-6">{options.message}</p>
        <div className="flex justify-end gap-3">
          <button
            onClick={() => resolve(false)}
            className="btn btn-sm bg-gray-800 hover:bg-gray-700 text-gray-200"
            autoFocus
          >
            {options.cancelLabel || 'Cancel'}
          </button>
          <button
            onClick={() => resolve(true)}
            className={`btn btn-sm text-white ${
              options.danger === false ? 'bg-blue-600 hover:bg-blue-500' : 'bg-red-600 hover:bg-red-500'
            }`}
          >
            {options.confirmLabel || 'Delete'}
          </button>
        </div>
      </div>
    </div>
  )

  return { confirm, ConfirmDialog }
}
