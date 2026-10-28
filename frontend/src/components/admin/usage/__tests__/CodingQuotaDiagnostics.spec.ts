import { mount, flushPromises } from '@vue/test-utils'
import { describe,it,expect,vi } from 'vitest'
import CodingQuotaDiagnostics from '../CodingQuotaDiagnostics.vue'
import {listCodingReservations} from '@/api/admin/usageMonitor'
vi.mock('@/api/admin/usageMonitor',()=>({listCodingReservations:vi.fn().mockResolvedValue({items:[],next_cursor:'page-2'})}))
vi.mock('vue-i18n',async()=>({...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),useI18n:()=>({t:(key:string)=>key})}))
describe('CodingQuotaDiagnostics',()=>{
 it('loads read-only request details only on demand and preserves filters for pagination',async()=>{
  vi.clearAllMocks()
  const wrapper=mount(CodingQuotaDiagnostics,{props:{userId:1,purpose:'openai',summary:{active:1,settled:'2',pending:'3',reserved:'4',admission_mode:'single',reason:'uncalibrated',mode:'shadow',pending_tasks:0,failed_tasks:0,unresolved:0,corrections:'0',reliable:false,rate:'',samples:0,observed_at:null,user_id:1,purpose:'openai'}}})
  expect(listCodingReservations).not.toHaveBeenCalled()
  await wrapper.get('[data-testid="coding-details-toggle"]').trigger('click');await flushPromises()
  expect(listCodingReservations).toHaveBeenCalledWith({user_id:1,purpose:'openai',state:'',limit:50,before:''})
  await wrapper.get('[data-testid="coding-details-more"]').trigger('click');await flushPromises()
  expect(listCodingReservations).toHaveBeenLastCalledWith({user_id:1,purpose:'openai',state:'',limit:50,before:'page-2'})
  await wrapper.get('select').setValue('unresolved');await flushPromises()
  expect(listCodingReservations).toHaveBeenLastCalledWith({user_id:1,purpose:'openai',state:'unresolved',limit:50,before:''})
 })
})
