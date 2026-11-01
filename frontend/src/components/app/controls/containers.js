import {layerContext, dialogDepthContext, nextDialogLayer} from '@/components/app/layers'
import {computed, defineComponent, Fragment, h, nextTick, provide, ref, useId, watch} from 'vue'
import {Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle} from '@/components/ui/dialog'
import {Accordion, AccordionContent, AccordionItem, AccordionTrigger} from '@/components/ui/accordion'
import {Tabs, TabsContent, TabsList, TabsTrigger} from '@/components/ui/tabs'
import {DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger} from '@/components/ui/dropdown-menu'
import {Tooltip, TooltipContent, TooltipProvider, TooltipTrigger} from '@/components/ui/tooltip'
import {Popover, PopoverContent, PopoverTrigger} from '@/components/ui/popover'
import {Label} from '@/components/ui/label'
import {Button} from '@/components/ui/button'
import {cn} from '@/lib/utils'
import {controlProps, dimension, fieldContext, flatten, nodeText} from './context'
import {inject} from 'vue'

export const AppModal = defineComponent({name:'AppModal',inheritAttrs:false,props:{...controlProps(['modelValue','title','width','description','showClose','closeOnClickModal','closeOnPressEscape']),showClose:{type:Boolean,default:true},closeOnClickModal:{type:Boolean,default:true},closeOnPressEscape:{type:Boolean,default:true}},emits:['update:modelValue','open','opened','close','closed'],setup(props,{attrs,slots,emit}){
 const depth=inject(dialogDepthContext,0)+1
 provide(dialogDepthContext,depth)
 const layer=ref(1000);provide(layerContext,layer)
 watch(()=>props.modelValue,(open,previous,onCleanup)=>{
  if(open){layer.value=nextDialogLayer();emit('open');nextTick(()=>{if(props.modelValue)emit('opened')})}
  else if(previous){emit('close');const timer=setTimeout(()=>emit('closed'),200);onCleanup(()=>clearTimeout(timer))}
 },{immediate:true})
 return()=>h(Dialog,{open:Boolean(props.modelValue),'onUpdate:open':value=>emit('update:modelValue',value)},{default:()=>h(DialogContent,{
  ...attrs,layerIndex:layer.value,dialogDepth:depth,showCloseButton:props.showClose,class:cn('app-modal',attrs.class),style:{width:props.width&&props.width!=='auto'?dimension(props.width):'min(760px, calc(100vw - 2rem))',maxWidth:'calc(100vw - 2rem)',minWidth:0},
  onPointerDownOutside:event=>{if(!props.closeOnClickModal)event.preventDefault()},onEscapeKeyDown:event=>{if(!props.closeOnPressEscape)event.preventDefault()},
 },{default:()=>[h(DialogHeader,{class:'app-dialog-header'},()=>[h(DialogTitle,{class:'app-dialog-title'},()=>props.title||'查看详情'),h(DialogDescription,{class:props.description?'text-sm':'sr-only'},()=>props.description||'查看相关内容并完成当前操作')]),h('div',{class:'app-dialog-body'},slots.default?.()),slots.footer?h('div',{class:'app-dialog-footer'},slots.footer()):null]})})
}})
export const AppForm=defineComponent({name:'AppForm',props:controlProps(['model','rules','labelWidth','labelPosition']),setup:(_,{attrs,slots})=>()=>h('form',{...attrs,class:cn('space-y-4',attrs.class)},slots.default?.())})
export const AppField=defineComponent({name:'AppField',props:controlProps(['label','required','labelWidth']),setup(props,{attrs,slots}){
 const firstId=ref(''),labelId=useId();provide(fieldContext,{firstId,labelId})
 return()=>h('div',{...attrs,class:cn('app-field grid min-w-0 gap-2',attrs.class)},[props.label?h(Label,{id:labelId,for:firstId.value||undefined,class:'text-sm font-medium text-foreground'},()=>[props.label,props.required?h('span',{class:'ml-1 text-destructive','aria-hidden':true},'*'):null]):null,h('div',{class:'min-w-0'},slots.default?.())])
}})
export const AppFold=defineComponent({name:'AppFold',props:controlProps(['modelValue','accordion']),emits:['update:modelValue','change'],setup(props,{attrs,slots,emit}){
 const active=ref(props.modelValue||[])
 watch(()=>props.modelValue,value=>{if(value!=null)active.value=value})
 return()=>h(Accordion,{...attrs,type:props.accordion?'single':'multiple',collapsible:true,modelValue:props.accordion?Array.isArray(active.value)?active.value[0]||'':String(active.value||''):Array.isArray(active.value)?active.value.map(String):active.value?[String(active.value)]:[],class:cn('app-fold w-full',attrs.class),'onUpdate:modelValue':value=>{active.value=value;emit('update:modelValue',value);emit('change',value)}},slots)
}})
export const AppFoldItem=defineComponent({name:'AppFoldItem',props:controlProps(['name','title','disabled']),setup:(props,{attrs,slots})=>()=>h(AccordionItem,{...attrs,value:String(props.name),disabled:props.disabled,class:cn('app-fold-item border-b border-border',attrs.class)},{default:()=>[h(AccordionTrigger,{class:'py-4 text-sm hover:no-underline'},slots.title||(()=>props.title)),h(AccordionContent,{class:'pb-5'},slots.default)]})})
export const AppTab=defineComponent({name:'AppTab',props:controlProps(['name','label','lazy','closable']),setup:()=>()=>null})
export const AppTabs=defineComponent({name:'AppTabs',props:controlProps(['modelValue','type','tabPosition']),emits:['update:modelValue','tabChange','tabRemove'],setup(props,{attrs,slots,emit}){
 const active=ref(props.modelValue),seen=ref(new Set())
 watch(()=>props.modelValue,value=>{active.value=value;seen.value.add(String(value))},{immediate:true})
 return()=>{
  const panes=flatten(slots.default?.()).filter(node=>node.type===AppTab)
  const value=String(active.value??panes[0]?.props?.name??'')
  return h(Tabs,{...attrs,modelValue:value,class:cn('app-tabs flex min-h-0 min-w-0 flex-col gap-4',attrs.class),'onUpdate:modelValue':next=>{active.value=next;seen.value.add(next);emit('update:modelValue',next);emit('tabChange',next)}},{default:()=>[
   h(TabsList,{class:'app-tabs-list h-auto w-fit max-w-full shrink-0 flex-wrap justify-start gap-1 rounded-xl bg-muted p-1'},()=>panes.map((pane,index)=>{
    const name=String(pane.props?.name??index),closable=pane.props?.closable!==undefined&&pane.props?.closable!==false
    const trigger=h(TabsTrigger,{value:name,class:cn('h-9 flex-none rounded-lg px-3 text-sm',closable?'pr-9':null)},pane.children?.label||(()=>pane.props?.label))
    return closable?h('div',{class:'relative flex items-center'},[trigger,h(Button,{type:'button',variant:'ghost',size:'icon',class:'absolute right-1 size-6 text-muted-foreground','aria-label':`关闭 ${pane.props?.label||nodeText(pane.children?.label?.())||'此项'}`,onClick:event=>{event.stopPropagation();emit('tabRemove',name)}},()=> '×')]):trigger
   })),
   ...panes.map((pane,index)=>{
    const name=String(pane.props?.name??index),visible=name===value
    if(pane.props?.lazy&&!visible&&!seen.value.has(name))return null
    return h(TabsContent,{key:name,value:name,forceMount:true,class:'app-tab-content mt-0 min-h-0 min-w-0 flex-1',style:visible?undefined:{display:'none'}},pane.children?.default)
   }),
  ]})
 }
}})
const menuContext=Symbol('menu-command')
export const AppDropdown=defineComponent({name:'AppDropdown',props:controlProps(['trigger','placement']),emits:['command','visibleChange'],setup(props,{attrs,slots,emit}){
 const layer=inject(layerContext,null)
 provide(menuContext,{command:value=>emit('command',value)})
 return()=>h(DropdownMenu,{'onUpdate:open':value=>emit('visibleChange',value)},{default:()=>[h(DropdownMenuTrigger,{asChild:true},slots.default),h(DropdownMenuContent,{align:props.placement?.includes('start')?'start':'end',style:{zIndex:(layer?.value||60)+20},class:cn('min-w-44',attrs.class)},slots.dropdown)]})
}})
export const AppDropdownMenu=defineComponent({name:'AppDropdownMenu',setup:(_,{slots})=>()=>h('div',{},slots.default?.())})
export const AppDropdownItem=defineComponent({name:'AppDropdownItem',props:controlProps(['command','disabled','divided']),emits:['click'],setup(props,{attrs,slots,emit}){
 const menu=inject(menuContext,null)
 return()=>h(Fragment,{},[props.divided?h(DropdownMenuSeparator):null,h(DropdownMenuItem,{...attrs,disabled:props.disabled,onSelect:event=>{emit('click',event);if(props.command!=null)menu?.command(props.command)}},slots)])
}})
export const AppTooltip=defineComponent({name:'AppTooltip',props:controlProps(['content','placement','disabled']),setup:(props,{slots})=>{const layer=inject(layerContext,null);return()=>h(TooltipProvider,{delayDuration:250},()=>h(Tooltip,{disabled:props.disabled},()=>[h(TooltipTrigger,{asChild:true},slots.default),h(TooltipContent,{style:{zIndex:(layer?.value||60)+30},side:props.placement?.split('-')[0]||'top',class:'max-w-sm whitespace-pre-wrap break-words'},slots.content||(()=>props.content))]))}})
export const AppPopconfirm=defineComponent({name:'AppPopconfirm',props:controlProps(['title']),emits:['confirm','cancel'],setup(props,{slots,emit}){
 const open=ref(false),layer=inject(layerContext,null)
 return()=>h(Popover,{open:open.value,'onUpdate:open':value=>{open.value=value}},{default:()=>[h(PopoverTrigger,{asChild:true},slots.reference||slots.default),h(PopoverContent,{class:'w-64',style:{zIndex:(layer?.value||60)+20}},()=>[h('p',{class:'mb-4 text-sm'},props.title),h('div',{class:'flex justify-end gap-2'},[h(Button,{variant:'outline',size:'sm',onClick:()=>{open.value=false;emit('cancel')}},()=> '取消'),h(Button,{variant:'destructive',size:'sm',onClick:()=>{open.value=false;emit('confirm')}},()=> '确认')])])]})
}})
