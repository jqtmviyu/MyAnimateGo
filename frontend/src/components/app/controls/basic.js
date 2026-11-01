import {controlProps} from "./context"
import {computed, defineComponent, h, inject, ref} from 'vue'
import {LoaderCircle, X, Eye, EyeOff, ChevronDown, ChevronUp, Minus, Plus} from '@lucide/vue'
import {Button} from '@/components/ui/button'
import {Input} from '@/components/ui/input'
import {Textarea} from '@/components/ui/textarea'
import {Switch} from '@/components/ui/switch'
import {Checkbox} from '@/components/ui/checkbox'
import {icons} from '@/components/app/icons'
import {cn} from '@/lib/utils'
import {checkboxContext, useField} from './context'
import {nodeText} from './context'

const iconLabels = {Search: '搜索', Refresh: '刷新', RefreshRight: '刷新', Close: '关闭', Delete: '删除', Plus: '添加', DocumentAdd: '使用此项', DocumentCopy: '复制', Upload: '上传', Download: '下载', Edit: '编辑', MoreFilled: '更多操作', FolderOpened: '打开目录'}
export const AppIcon = defineComponent({name: 'AppIcon', props: controlProps(['name']), setup: (props, {slots, attrs}) => () => {
  const icon = typeof props.name === 'string' ? icons[props.name] : props.name
  return h('span', {class: cn('inline-flex shrink-0 items-center justify-center [&>svg]:size-4', attrs.class), style: attrs.style, 'aria-hidden': true}, icon ? [h(icon)] : slots.default?.())
}})
export const AppButton = defineComponent({name: 'AppButton', inheritAttrs: false,
  props: controlProps(['type', 'nativeType', 'size', 'icon', 'loading', 'disabled', 'text', 'bg', 'circle', 'plain', 'link']),
  setup(props, {attrs, slots}) {
    return () => {
      let variant = props.type === 'primary' ? 'default' : props.type === 'danger' ? 'destructive' : 'outline'
      if (props.text || props.link) variant = props.bg ? 'secondary' : 'ghost'
      return h(Button, {...attrs, type: props.nativeType || 'button', variant, size: props.circle ? 'icon' : props.size === 'small' ? 'sm' : 'default', disabled: props.disabled || props.loading,
        'aria-label': attrs['aria-label'] || nodeText(slots.default?.()) || iconLabels[props.icon] || (props.icon ? '操作' : undefined), 'aria-busy': Boolean(props.loading),
        class: cn('app-button', {'rounded-full': props.circle, 'text-destructive': props.type === 'danger' && props.text, 'text-primary': props.type === 'primary' && props.text}, attrs.class)},
      {default: () => [props.loading ? h(LoaderCircle, {class: 'size-4 animate-spin'}) : slots.icon ? slots.icon() : props.icon ? h(AppIcon, {name: props.icon}) : null, slots.default?.()]})
    }
  },
})
export const AppInput = defineComponent({name: 'AppInput', inheritAttrs: false,
  props: controlProps(['modelValue', 'modelModifiers', 'type', 'placeholder', 'disabled', 'clearable', 'showPassword', 'prefixIcon', 'suffixIcon', 'rows', 'autosize']), emits: ['update:modelValue', 'input', 'change', 'clear'],
  setup(props, {attrs, slots, emit, expose}) {
    const field = useField(attrs), visible = ref(false), element = ref(null)
    expose({focus: () => element.value?.$el?.focus(), blur: () => element.value?.$el?.blur(), select: () => element.value?.$el?.select()})
    const update = value => { const next = props.modelModifiers?.trim ? String(value).trim() : value; emit('update:modelValue', next); emit('input', next) }
    return () => {
      const {class: className, style, ...rest} = attrs
      const prefix = slots.prefix?.() || (props.prefixIcon ? h(AppIcon, {name: props.prefixIcon}) : null)
      const suffix = slots.suffix?.() || (props.suffixIcon ? h(AppIcon, {name: props.suffixIcon}) : null)
      const textarea = props.type === 'textarea'
      return h('div', {class: cn('app-input relative flex w-full min-w-0 items-center', className), style}, [
        prefix && !textarea ? h('span', {class: 'pointer-events-none absolute left-3 z-10 text-muted-foreground'}, prefix) : null,
        h(textarea ? Textarea : Input, {...rest, ...field, ref: element, class: cn('app-input__inner w-full', {'pl-9': prefix && !textarea, 'pr-10': suffix || props.clearable || props.showPassword}),
          type: props.showPassword ? visible.value ? 'text' : 'password' : props.type || 'text', modelValue: props.modelValue ?? '', rows: props.rows || props.autosize?.minRows || 4, disabled: props.disabled, placeholder: props.placeholder,
          'onUpdate:modelValue': update, onChange: () => emit('change', props.modelValue)}),
        suffix ? h('span', {class: 'app-input__suffix absolute right-3 text-muted-foreground'}, suffix) : null,
        props.showPassword ? h(Button, {variant: 'ghost', size: 'icon', type: 'button', class: 'absolute right-1 size-7', 'aria-label': visible.value ? '隐藏密码' : '显示密码', onClick: () => { visible.value = !visible.value }}, () => h(visible.value ? EyeOff : Eye, {class: 'size-4'})) : null,
        props.clearable && props.modelValue && !props.disabled ? h(Button, {variant: 'ghost', size: 'icon', type: 'button', class: 'absolute right-1 size-7', 'aria-label': '清空输入', onClick: () => {update(''); emit('clear')}}, () => h(X, {class: 'size-3.5'})) : null,
      ])
    }
  },
})
export const AppNumber = defineComponent({name: 'AppNumber', inheritAttrs: false,
  props: controlProps(['modelValue', 'min', 'max', 'step', 'precision', 'disabled', 'controlsPosition']), emits: ['update:modelValue', 'change'],
  setup(props, {attrs, slots, emit}) {
    const field = useField(attrs)
    const set = raw => {
      let value = Number(raw)
      if (!Number.isFinite(value)) return
      if (props.min != null) value = Math.max(Number(props.min), value)
      if (props.max != null) value = Math.min(Number(props.max), value)
      if (props.precision != null) value = Number(value.toFixed(props.precision))
      emit('update:modelValue', value); emit('change', value)
    }
    return () => h('div', {class: cn('app-number inline-flex max-w-full items-center gap-1', attrs.class), style: attrs.style}, [
      h(Button, {type: 'button', variant: 'outline', size: 'icon', class: 'h-[38px] w-[38px] shrink-0', disabled: props.disabled || props.min != null && Number(props.modelValue) <= Number(props.min), 'aria-label': '减少数值', onClick: () => set(Number(props.modelValue || 0) - Number(props.step || 1))}, () => h(Minus, {class: 'size-3'})),
      h(Input, {...field, type: 'number', class: 'min-w-0 text-center [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none', modelValue: props.modelValue, min: props.min, max: props.max, step: props.step || 1, disabled: props.disabled, 'onUpdate:modelValue': set}),
      h(Button, {type: 'button', variant: 'outline', size: 'icon', class: 'h-[38px] w-[38px] shrink-0', disabled: props.disabled || props.max != null && Number(props.modelValue) >= Number(props.max), 'aria-label': '增加数值', onClick: () => set(Number(props.modelValue || 0) + Number(props.step || 1))}, () => h(Plus, {class: 'size-3'})),
      slots.suffix ? h('span', {class: 'app-input__suffix shrink-0 pl-1 text-[14px] text-muted-foreground'}, slots.suffix()) : null,
    ])
  },
})
export const AppSwitch = defineComponent({name: 'AppSwitch', inheritAttrs: false, props: controlProps(['modelValue', 'disabled', 'activeText', 'inactiveText']), emits: ['update:modelValue', 'change'], setup(props, {attrs, emit}) {
  const field = useField(attrs)
  return () => h('span', {class: 'app-switch inline-flex items-center gap-2'}, [h(Switch, {...attrs, ...field, disabled: props.disabled, modelValue: Boolean(props.modelValue), 'onUpdate:modelValue': value => {emit('update:modelValue', value);emit('change', value)}}), props.activeText || props.inactiveText ? h('span', {class: 'text-xs text-muted-foreground'}, props.modelValue ? props.activeText : props.inactiveText) : null])
}})
export const AppCheckbox = defineComponent({name: 'AppCheckbox', inheritAttrs: false, props: controlProps(['modelValue', 'value', 'label', 'disabled', 'indeterminate']), emits: ['update:modelValue', 'change'], setup(props, {attrs, emit, slots}) {
  const field = useField(attrs), group = inject(checkboxContext, null)
  const value = computed(() => props.value ?? props.label)
  const checked = computed(() => props.indeterminate ? 'indeterminate' : group ? (group.value.value || []).includes(value.value) : Boolean(props.modelValue))
  const update = next => {
    if (group) { const values = (group.value.value || []).filter(v => v !== value.value); if (next) values.push(value.value); group.update(values) }
    else { emit('update:modelValue', next === true); emit('change', next === true) }
  }
  return () => h('label', {class: cn('app-checkbox inline-flex min-h-8 cursor-pointer items-center gap-[8px] text-sm', attrs.class), style: attrs.style}, [h(Checkbox, {...attrs, ...field, modelValue: checked.value, disabled: props.disabled || group?.disabled.value, 'aria-label': attrs['aria-label'] || nodeText(slots.default?.()) || props.label, 'aria-labelledby': nodeText(slots.default?.()) || props.label ? undefined : field['aria-labelledby'], 'onUpdate:modelValue': update}), slots.default?.() || (props.label ? h('span', String(props.label)) : null)])
}})
