import {layerContext} from '@/components/app/layers'
import {controlProps} from "./context"
import {computed, defineComponent, h, inject, onDeactivated, provide, ref, useId} from 'vue'
import {X, Plus, Star} from '@lucide/vue'
import {Select, SelectContent, SelectItem, SelectTrigger, SelectValue} from '@/components/ui/select'
import {RadioGroup, RadioGroupItem} from '@/components/ui/radio-group'
import {Popover, PopoverContent, PopoverTrigger} from '@/components/ui/popover'
import {Input} from '@/components/ui/input'
import {Label} from '@/components/ui/label'
import {Button} from '@/components/ui/button'
import {Badge} from '@/components/ui/badge'
import {cn} from '@/lib/utils'
import {checkboxContext, choiceContext, emptyChoice, useField, nodeText, flatten} from './context'

export const AppCheckboxGroup = defineComponent({name: 'AppCheckboxGroup', props: controlProps(['modelValue', 'disabled']), emits: ['update:modelValue', 'change'], setup(props, {slots, emit, attrs}) {
  provide(checkboxContext, {value: computed(() => props.modelValue || []), disabled: computed(() => props.disabled), update: value => {emit('update:modelValue', value);emit('change', value)}})
  return () => h('div', {...attrs, class: cn('flex flex-wrap items-center gap-x-5 gap-y-2', attrs.class)}, slots.default?.())
}})
export const AppRadioGroup = defineComponent({name: 'AppRadioGroup', props: controlProps(['modelValue', 'disabled']), emits: ['update:modelValue', 'change'], setup(props, {slots, emit, attrs}) {
  const field = useField(attrs)
  return () => h(RadioGroup, {...attrs, ...field, class: cn('flex flex-wrap items-center gap-4', attrs.class), modelValue: props.modelValue, disabled: props.disabled, 'onUpdate:modelValue': value => {emit('update:modelValue', value);emit('change', value)}}, slots)
}})
export const AppRadio = defineComponent({name: 'AppRadio', props: controlProps(['value', 'label', 'disabled']), setup(props, {slots, attrs}) {
  const id = useId()
  return () => h('div', {class: cn('inline-flex min-h-8 items-center gap-2', attrs.class), style: attrs.style}, [h(RadioGroupItem, {id, value: props.value ?? props.label, disabled: props.disabled, 'aria-label': nodeText(slots.default?.()) || props.label}), h(Label, {for: id, class: 'cursor-pointer text-sm font-normal'}, slots.default || (() => props.label))])
}})
export const AppSelect = defineComponent({name: 'AppSelect', inheritAttrs: false, props: controlProps(['modelValue', 'placeholder', 'disabled', 'clearable', 'multiple', 'filterable', 'allowCreate']), emits: ['update:modelValue', 'change', 'clear'], setup(props, {slots, attrs, emit}) {
  const field = useField(attrs), query = ref(''), open = ref(false), layer=inject(layerContext,null)
  onDeactivated(() => { open.value = false })
  const update = value => { const next = props.multiple ? value : value === emptyChoice ? '' : value; emit('update:modelValue', next);emit('change', next) }
  provide(choiceContext, {query, multiple: computed(() => props.multiple)})
  const create = () => {const value = query.value.trim();if (!value) return;update(props.multiple ? [...new Set([...(props.modelValue || []), value])] : value);query.value = '';open.value=false}
  return () => {
    const selected = props.multiple ? props.modelValue || [] : props.modelValue === '' ? emptyChoice : props.modelValue
    const options = slots.default?.()
    const labels = new Map(flatten(options).filter(node => node.props && 'value' in node.props).map(node => [node.props.value, String(node.props.label ?? node.props.value)]))
    const selectedLabels = props.multiple ? (props.modelValue || []).map(value => labels.get(value) ?? String(value)) : []
    const summary = selectedLabels.length > 2 ? `${selectedLabels[0]} +${selectedLabels.length - 1}` : selectedLabels.join('、')
    const canClear = props.clearable && (props.multiple ? props.modelValue?.length : props.modelValue !== '' && props.modelValue != null) && !props.disabled
    return h('div', {class: cn('app-select relative min-w-0', attrs.class), style: attrs.style}, [h(Select, {open:open.value, 'onUpdate:open':value=>{open.value=value}, modelValue: selected, disabled: props.disabled, multiple: props.multiple, 'onUpdate:modelValue': update}, {
      default: () => [h(SelectTrigger, {...field, class: cn('w-full min-w-0', {'pr-[60px]': canClear}), title: selectedLabels.join('、') || undefined, 'aria-label': attrs['aria-label'] || props.placeholder}, {default: () => h(SelectValue, {class: 'min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap', placeholder: props.placeholder || '请选择'}, selectedLabels.length ? () => summary : props.allowCreate && props.modelValue ? () => String(props.modelValue) : undefined)}),
        h(SelectContent, {class: 'app-select-content',style:{zIndex:(layer?.value||60)+20}}, {default: () => [props.filterable || props.allowCreate ? h('div', {class: 'sticky top-0 z-10 bg-popover p-2'}, [h(Input, {modelValue: query.value, placeholder: props.allowCreate ? '搜索或输入新值' : '搜索选项', 'aria-label': '搜索选项', 'onUpdate:modelValue': value => {query.value = String(value)}, onKeydown: event => {if (event.key === 'Enter' && props.allowCreate) {event.preventDefault();event.stopPropagation();create()} else if (event.key.length === 1 || event.key === ' ') event.stopPropagation()}}), props.allowCreate && query.value ? h(Button, {type: 'button', variant: 'ghost', size: 'sm', class: 'mt-1 w-full justify-start', onClick: create}, () => `使用“${query.value}”`) : null]) : null, options]})],
    }), canClear ? h(Button, {type: 'button', variant: 'ghost', size: 'icon', class: 'absolute right-[32px] top-1/2 h-[20px] w-[20px] -translate-y-1/2 rounded-full p-0 text-muted-foreground hover:bg-secondary', 'aria-label': '清空选择', onClick: () => {update(props.multiple ? [] : '');emit('clear')}}, () => h(X, {class: 'size-4'})) : null])
  }
}})
export const AppOption = defineComponent({name: 'AppOption', props: controlProps(['value', 'label', 'disabled']), setup(props, {slots}) {
  const context = inject(choiceContext, null)
  return () => {
    const label = String(props.label ?? props.value ?? '')
    if (context?.query.value && !label.toLowerCase().includes(context.query.value.toLowerCase())) return null
    return h(SelectItem, {multiple: Boolean(context?.multiple.value), value: props.value === '' ? emptyChoice : props.value, disabled: props.disabled, textValue: label}, {default: slots.default || (() => label)})
  }
}})
export const AppTagsInput = defineComponent({name: 'AppTagsInput', props: controlProps(['modelValue', 'placeholder', 'disabled']), emits: ['update:modelValue', 'change'], setup(props, {attrs, emit}) {
  const draft = ref(''), field = useField(attrs)
  const update = value => {emit('update:modelValue', value);emit('change', value)}
  const add = () => {const value = draft.value.trim();if (value && !(props.modelValue || []).includes(value)) update([...(props.modelValue || []), value]);draft.value = ''}
  return () => h('div', {'data-focus-container': '', class: cn('app-tags-input flex min-w-0 flex-1 flex-wrap items-center gap-2 rounded-lg border border-input bg-transparent p-2 focus-within:border-ring focus-within:ring-2 focus-within:ring-inset focus-within:ring-ring/50', attrs.class)}, [
    ...(props.modelValue || []).map((value, index) => h(Badge, {variant: 'secondary', class: 'app-tag gap-1.5'}, () => [String(value), h('button', {type: 'button', class: 'app-tag-remove', disabled: props.disabled, 'aria-label': `移除 ${value}`, onClick: () => update(props.modelValue.filter((_, i) => i !== index))}, [h(X, {class: 'size-4'})])])),
    h(Input, {...field, modelValue: draft.value, class: 'h-7 min-w-24 flex-1 border-0 px-1 shadow-none focus-visible:ring-0', placeholder: props.placeholder || '输入后按 Enter 添加', disabled: props.disabled, 'onUpdate:modelValue': value => {draft.value = value}, onKeydown: event => {if (event.key === 'Enter') {event.preventDefault();add()}}}),
  ])
}})
export const AppDate = defineComponent({name: 'AppDate', props: controlProps(['modelValue', 'disabled']), emits: ['update:modelValue', 'change'], setup(props, {attrs, emit}) {
  const field = useField(attrs)
  const value = () => {if (!props.modelValue) return '';if (typeof props.modelValue === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(props.modelValue)) return props.modelValue;const date = new Date(props.modelValue);return Number.isNaN(date.getTime()) ? '' : `${date.getFullYear()}-${String(date.getMonth()+1).padStart(2,'0')}-${String(date.getDate()).padStart(2,'0')}`}
  return () => h(Input, {...attrs, ...field, type: 'date', modelValue: value(), disabled: props.disabled, 'onUpdate:modelValue': next => {emit('update:modelValue', next);emit('change', next)}})
}})
export const AppColor = defineComponent({name: 'AppColor', props: controlProps(['modelValue', 'predefine']), emits: ['update:modelValue', 'change', 'activeChange'], setup(props, {attrs, emit}) {
  const field = useField(attrs)
  const update = value => {emit('update:modelValue', value);emit('change', value);emit('activeChange', value)}
  return () => h('div', {class: 'inline-flex flex-wrap items-center gap-2'}, [h('input', {...field, type: 'color', class: 'size-10 cursor-pointer rounded-lg border border-input bg-card p-1', value: props.modelValue || '#18765b', 'aria-label': '选择强调色', onInput: event => update(event.target.value)}), ...(props.predefine || []).map(color => h('button', {type: 'button', class: 'size-6 rounded-full border-2 border-background ring-1 ring-border', style: {background: color}, 'aria-label': `使用颜色 ${color}`, onClick: () => update(color)}))])
}})
export const AppRate = defineComponent({name: 'AppRate', props: controlProps(['modelValue', 'max', 'disabled']), emits: ['update:modelValue', 'change'], setup(props, {emit}) {
  return () => h('div', {class: 'flex flex-wrap gap-1', role: 'group', 'aria-label': '评分'}, Array.from({length: props.max || 5}, (_, index) => h(Button, {variant: 'ghost', type: 'button', size: 'icon', class: 'size-6 p-0', disabled: props.disabled, 'aria-label': `${index+1} 分`, 'aria-pressed': Number(props.modelValue) === index+1, onClick: () => {emit('update:modelValue', index+1);emit('change', index+1)}}, () => h(Star, {class: cn('size-5 text-amber-500', {'fill-amber-500': Number(props.modelValue) > index})}))))
}})
