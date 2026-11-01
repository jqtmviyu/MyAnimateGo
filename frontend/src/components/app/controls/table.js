import {computed, defineComponent, h, ref, watch} from 'vue'
import {Checkbox} from '@/components/ui/checkbox'
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow} from '@/components/ui/table'
import {cn} from '@/lib/utils'
import {AppEmpty} from './surfaces'
import {controlProps, dimension, flatten} from './context'

export const AppColumn=defineComponent({name:'AppColumn',setup:()=>()=>null})
export const AppTable=defineComponent({name:'AppTable',inheritAttrs:false,props:{...controlProps(['data','height','size','stripe','rowKey','showHeader']),showHeader:{type:Boolean,default:true}},emits:['selectionChange','rowClick','currentChange'],setup(props,{attrs,slots,emit,expose}){
 const selected=ref(new Set()),rows=computed(()=>props.data||[])
 const key=row=>props.rowKey?row[props.rowKey]:row.id??row.hash??row
 const selectedRows=()=>rows.value.filter(row=>selected.value.has(key(row)))
 const notify=()=>emit('selectionChange',selectedRows())
 const toggle=(row,value)=>{const next=new Set(selected.value),id=key(row);if(value??!next.has(id))next.add(id);else next.delete(id);selected.value=next;notify()}
 const clear=()=>{selected.value=new Set();notify()}
 const all=value=>{selected.value=new Set(value?rows.value.map(key):[]);notify()}
 watch(()=>props.data,clear)
 expose({clearSelection:clear,toggleRowSelection:toggle,getSelectionRows:selectedRows})
 return()=>{
  const columns=flatten(slots.default?.()).filter(node=>node.type===AppColumn)
  let left=0
  const columnStyles=columns.map(column=>{
   const p=column.props||{},style={width:dimension(p.width),minWidth:dimension(p.minWidth??p['min-width']??p.width??(p.type==='selection'?44:100))}
   if(p.fixed!==undefined&&p.fixed!==false){style.position='sticky';style.left=`${left}px`;style.zIndex=1;left+=parseInt(p.width||p.minWidth||100)}
   if(p.align)style.textAlign=p.align
   return style
  })
  const count=selectedRows().length
  return h('div',{...attrs,class:cn('app-table min-w-0 overflow-auto rounded-xl border border-border bg-card',attrs.class),style:{maxHeight:dimension(props.height),...attrs.style}},[
   h(Table,{class:'w-full text-sm'},{default:()=>[
    props.showHeader!==false?h(TableHeader,{class:'sticky top-0 z-20 bg-muted/80 backdrop-blur'},{default:()=>h(TableRow,{},()=>columns.map((column,index)=>{
     const p=column.props||{}
     return h(TableHead,{key:index,class:cn('h-11 whitespace-nowrap px-4 text-xs font-medium text-muted-foreground',p.fixed!==undefined&&p.fixed!==false?'bg-muted':null),style:columnStyles[index]},()=>p.type==='selection'?h(Checkbox,{modelValue:count===rows.value.length&&count>0?true:count?'indeterminate':false,'aria-label':'选择所有行','onUpdate:modelValue':all}):column.children?.header?.({column:p,$index:index})||p.label)
    }))}):null,
    h(TableBody,{},()=>rows.value.length?rows.value.map((row,rowIndex)=>h(TableRow,{key:key(row),class:cn('group hover:bg-muted/50',selected.value.has(key(row))?'bg-accent/40':null),'data-state':selected.value.has(key(row))?'selected':undefined,onClick:()=>{emit('rowClick',row);emit('currentChange',row)}},()=>columns.map((column,index)=>{
     const p=column.props||{},slot=column.children?.default
     return h(TableCell,{key:index,class:cn('px-4 py-3 align-middle',p.fixed!==undefined&&p.fixed!==false?'bg-card':null),style:columnStyles[index]},()=>p.type==='selection'?h(Checkbox,{modelValue:selected.value.has(key(row)),'aria-label':`选择 ${row.title||row.name||'当前行'}`,'onUpdate:modelValue':value=>toggle(row,value),onClick:event=>event.stopPropagation()}):p.type==='index'?rowIndex+1:slot?slot({row,$index:rowIndex,column:p}):row[p.prop]==null?'':String(row[p.prop]))
    }))):h(TableRow,{},()=>h(TableCell,{colspan:Math.max(columns.length,1),class:'p-0'},()=>slots.empty?.()||h(AppEmpty,{description:'暂无数据'})))),
   ]}),
  ])
 }
}})
