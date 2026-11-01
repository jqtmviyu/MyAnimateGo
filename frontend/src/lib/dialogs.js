import {nextTick, shallowRef} from 'vue'

export const activeDialog = shallowRef(null)
const queue = []
let closing = false
let sequence = 0

function showNext() {
  if (!closing && !activeDialog.value && queue.length) activeDialog.value = queue.shift()
}

function request(kind, message, title, options = {}) {
  if (typeof title === 'object') {
    options = title
    title = ''
  }
  const text = options.dangerouslyUseHTMLString
    ? String(message).replace(/<\/?strong[^>]*>/gi, '').replace(/<br\s*\/?>/gi, '\n').trim()
    : String(message)
  const promise = new Promise((resolve, reject) => {
    queue.push({id: ++sequence, kind, message: text, title: title || (kind === 'prompt' ? '输入内容' : '请确认'), options, resolve, reject})
    showNext()
  })
  // 直接展示的提示可忽略返回值；确认链仍保持取消拒绝语义。
  promise.catch(() => {})
  return promise
}

export function settleDialog(confirmed, value = '', action = 'cancel') {
  const current = activeDialog.value
  if (!current) return
  closing = true
  activeDialog.value = null
  if (confirmed) current.resolve(current.kind === 'prompt' ? {value, action: 'confirm'} : 'confirm')
  else current.reject(current.options.distinguishCancelAndClose ? action : 'cancel')
  nextTick(() => {
    closing = false
    showNext()
  })
}

export const dialogs = {
  confirm: (message, title, options) => request('confirm', message, title, options),
  alert: (message, title, options) => request('alert', message, title, {...options, showCancelButton: false}),
  prompt: (message, title, options) => request('prompt', message, title, options),
}
