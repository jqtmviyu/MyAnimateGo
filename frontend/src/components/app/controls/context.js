import {Fragment, Text, inject, onMounted, useId} from 'vue'

export const fieldContext = Symbol('field')
export const checkboxContext = Symbol('checkbox-group')
export const choiceContext = Symbol('choice-select')
export const emptyChoice = '__ani_rss_empty_choice__'
export const dimension = value => value == null ? undefined : /^\d+$/.test(String(value)) ? `${value}px` : value
export function useField(attrs) {
  const context = inject(fieldContext, null)
  const id = attrs.id || useId()
  onMounted(() => { if (context && !context.firstId.value) context.firstId.value = id })
  return {id, 'aria-labelledby': attrs['aria-label'] ? undefined : context?.labelId}
}
export function flatten(nodes) {
  return (nodes || []).flatMap(node => node.type === Fragment ? flatten(node.children) : [node])
}
export function nodeText(nodes) {
  return flatten(nodes).map(node => typeof node.children === 'string' ? node.children : Array.isArray(node.children) ? nodeText(node.children) : '').join('').trim()
}

const booleanProps = new Set(['divided','hidden','wrap','disabled','loading','text','bg','circle','plain','link','clearable','showPassword','multiple','filterable','allowCreate','indeterminate','border','accordion','stripe','closable','showIcon','lazy','round','readonly','showHeader','hideOnClick','showClose','closeOnClickModal','closeOnPressEscape','always','loop'])
export const controlProps = keys => Object.fromEntries(keys.map(key => [key, booleanProps.has(key) ? {type: Boolean, default: false} : {default: undefined}]))
