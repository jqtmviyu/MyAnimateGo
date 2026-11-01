import {computed, inject} from 'vue'
export const downloadPreferences=Symbol('download-preferences')
export function normalizeLayout(value={},columns=[],fallback={}) {
 const keys=columns.map(c=>c.key),has=key=>keys.includes(key)
 const order=[...new Set([...(value.order||[]).filter(has),...keys])]
 let visible=[...new Set((value.visible||fallback.visible||keys).filter(has))]
 for(const column of columns)if(column.required&&!visible.includes(column.key))visible.push(column.key)
 if(!visible.length&&keys.length)visible=[keys[0]]
 const widths={}
 for(const column of columns) {
  const width=value.widths?.[column.key]
  if(Number.isFinite(width))widths[column.key]=Math.max(column.minWidth||72,Math.min(1000,Math.round(width)))
 }
 const candidate=value.sort||fallback.sort||{}
 return {order,visible,widths,sort:{key:has(candidate.key)?candidate.key:keys[0],descending:Boolean(candidate.descending)}}
}
export function useTableLayout(name,definition,preferences=inject(downloadPreferences)) {
 const defs=computed(()=>typeof definition==='function'?definition():definition.value||definition)
 const layout=computed({
  get:()=>normalizeLayout(preferences.value.tables?.[name],defs.value,{visible:name==='tasks'?preferences.value.columns:undefined,sort:name==='tasks'?{key:'queuePosition'}:undefined}),
  set:value=>{
   const next=normalizeLayout(value,defs.value)
   preferences.value.tables={...preferences.value.tables,[name]:next}
   if(name==='tasks')preferences.value.columns=next.visible
  },
 })
 const columns=computed(()=>layout.value.order.filter(key=>layout.value.visible.includes(key)).map(key=>defs.value.find(c=>c.key===key)))
 const width=column=>layout.value.widths[column.key]||column.width||(column.key==='name'?360:120)
 const totalWidth=computed(()=>columns.value.reduce((sum,column)=>sum+width(column),0))
 function sort(key){const before=layout.value.sort;layout.value={...layout.value,sort:{key,descending:before.key===key?!before.descending:false}}}
 function resize(key,size){layout.value={...layout.value,widths:{...layout.value.widths,[key]:size}}}
 function reorder(source,target){const order=layout.value.order.filter(k=>k!==source);order.splice(order.indexOf(target),0,source);layout.value={...layout.value,order}}
 function toggle(key){const current=layout.value;layout.value={...current,visible:current.visible.includes(key)?current.visible.filter(k=>k!==key):[...current.visible,key]}}
 function value(row,column){const raw=row[column.key];return column.sortValue?column.sortValue(row):Array.isArray(raw)||raw===undefined?column.format?.(raw,row)??raw:raw}
 function sorted(rows){const {key,descending}=layout.value.sort,column=defs.value.find(c=>c.key===key);if(!column)return rows;return [...rows].sort((a,b)=>{const x=value(a,column),y=value(b,column);return (descending?-1:1)*(typeof x==='string'?x.localeCompare(String(y??''),'zh-CN'):(Number(x)||0)-(Number(y)||0))})}
 return {layout,columns,definitions:defs,width,totalWidth,sort,resize,reorder,toggle,sorted,reset:()=>{layout.value={}}}
}
