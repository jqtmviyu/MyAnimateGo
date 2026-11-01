// 每类数据独立调度；慢请求只影响自己的下一轮刷新。
export function createPollingChannel({enabled, interval, read, apply, fail, changed = () => {}}) {
  let running = false, timer, flight, revision = 0
  const state = {loading: false, updatedAt: 0, error: ''}
  const schedule = () => {
    clearTimeout(timer)
    if (running && enabled()) timer = setTimeout(refresh, state.error ? Math.max(interval(), 10000) : interval())
  }
  async function refresh() {
    clearTimeout(timer)
    if (!running || !enabled()) return
    if (flight) return flight.promise
    const current = {controller: new AbortController(), version: revision}
    flight = current
    state.loading = true
    changed(state)
    current.promise = (async () => {
      try {
        const value = await read(current.controller.signal)
        if (current.version !== revision || !running) return
        await apply(value)
        state.updatedAt = Date.now()
        state.error = ''
      } catch (error) {
        if (error.name !== 'AbortError' && current.version === revision && running) {
          state.error = error.message
          fail?.(error)
        }
      } finally {
        if (flight === current) {
          flight = null
          state.loading = false
          changed(state)
          schedule()
        }
      }
    })()
    return current.promise
  }
  function cancel() {
    revision++
    clearTimeout(timer)
    flight?.controller.abort()
    flight = null
    state.loading = false
    changed(state)
  }
  return {
    state, refresh,
    start() {running = true; return refresh()},
    stop() {running = false; cancel()},
    restart() {cancel(); return refresh()},
  }
}
