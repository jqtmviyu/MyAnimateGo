import {toast} from 'vue-sonner'

export const notify = Object.fromEntries(
  ['success', 'error', 'warning', 'info'].map(type => [type, (message, options = {}) => {
    const value = typeof message === 'object' ? message : {message, ...options}
    return toast[type](String(value.message ?? ''), {duration: value.duration ?? 4000})
  }]),
)
