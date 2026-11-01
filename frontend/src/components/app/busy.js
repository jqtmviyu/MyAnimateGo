const overlays = new WeakMap()

function update(element, binding) {
  const current = overlays.get(element)
  element.setAttribute('aria-busy', String(Boolean(binding.value)))
  if (!binding.value) {
    current?.overlay.remove()
    if (current?.lock) document.body.style.overflow = current.overflow
    if (current?.position != null) element.style.position = current.position
    overlays.delete(element)
    return
  }
  if (current) return
  const position = element.style.position
  const fullscreen = binding.modifiers.fullscreen
  const lock = binding.modifiers.lock
  const overflow = document.body.style.overflow
  if (lock) document.body.style.overflow = 'hidden'
  if (getComputedStyle(element).position === 'static') element.style.position = 'relative'
  const overlay = document.createElement('div')
  overlay.className = 'app-busy-indicator absolute inset-0 z-20 flex min-h-12 items-center justify-center gap-2 rounded-[inherit] bg-background/80 text-sm text-muted-foreground backdrop-blur-[1px]'
  if (fullscreen) { overlay.style.position = 'fixed'; overlay.style.zIndex = '10000' }
  overlay.setAttribute('role', 'status')
  const spinner = document.createElement('span')
  spinner.className = 'inline-block size-4 animate-spin rounded-full border-2 border-primary border-b-transparent'
  spinner.setAttribute('aria-hidden', 'true')
  overlay.append(spinner, document.createTextNode('加载中'))
  ;(fullscreen ? document.body : element).append(overlay)
  overlays.set(element, {overlay, position, lock, overflow})
}

export const busy = {mounted: update, updated: update, beforeUnmount(element) { const current = overlays.get(element); current?.overlay.remove(); if (current?.lock) document.body.style.overflow = current.overflow; overlays.delete(element) }}
