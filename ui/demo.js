'use strict';
window.__AGENT_DEMO=true;
(()=>{
 const id='11111111-2222-3333-4444-555555555555';
 const identity={server_name:'SQL-DEMO · 模拟实例',edition:'Developer (mock)',product_version:'16.0 · mock',login_name:'demo-user',is_sysadmin:1};
 const job={job_id:id,name:'Demo · 每日同步',description:'只读演示，不连接任何数据库。',enabled:0,start_step_id:1,version_number:1,owner_name:'demo-user',last_run_status:1,last_run_date:20261118,last_run_time:210000,last_run_duration:12,running:0,notify_level_email:0,notify_level_eventlog:2,notify_email_operator_id:0,delete_level:0};
 const steps=[{step_id:1,step_name:'更新业务数据',subsystem:'TSQL',database_name:'DemoDB',command:"SELECT N'只读演示';",on_success_action:1,on_success_step_id:0,on_fail_action:2,on_fail_step_id:0,retry_attempts:2,retry_interval:5}];
 const schedules=[{schedule_id:10,name:'每晚 21:00',enabled:1,freq_type:4,freq_interval:1,freq_subday_type:1,freq_subday_interval:0,active_start_date:20261119,active_end_date:99991231,active_start_time:210000,active_end_time:235959,attached_jobs:2}];
 const history=[{run_date:20261118,run_time:210000,step_id:0,step_name:'(Job outcome)',run_status:1,run_duration:12,retries_attempted:0,message:'模拟成功。该数据不是服务器查询结果。'}];
 const store=new Map();
 window.dbxPlugin={ready:Promise.resolve(),context:{},capabilities:{connectionsApi:true,storage:true},getConnections:async()=>[{id:'demo',name:'SQL-DEMO（只读模拟）',dbType:'sqlserver'}],storage:{get:async k=>store.get(k),set:async(k,v)=>store.set(k,v),delete:async k=>store.delete(k)},invoke:async(method,p)=>{
  if(method==='agent/token/status')return {supported:false,saved:false};
  if(method==='agent/start')throw Error('只读演示禁止执行；没有发送 SQL。');
  if(method==='agent/plan')return {id:'demo-plan',change:p.change,identity,sql:'-- MOCK PREVIEW ONLY · 不是实际后端生成结果\n-- 操作：'+p.change.action+'\n-- 演示模式禁止执行\n'+JSON.stringify(p.change,null,2),warnings:['只读模拟，不连接 DBX MCP 或数据库。'],confirmation:'demo / SQL-DEMO / '+p.change.action};
  if(method==='agent/export')return {identity,sql:"-- 模拟副本，仅供界面展示\n-- 新副本应默认禁用",warnings:['不是完整备份，真实导出请使用插件连接后的后端。']};
  if(method==='agent/read'){
   if(p.resource==='detail')return {identity,job:[job],steps,schedules,history};
   const resources={jobs:[job,{...job,job_id:'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee',name:'<img src=x onerror=alert(1)> · 字面文本',last_run_status:0}],history,steps,schedules,alerts:[{name:'Severity 20 · demo',enabled:1,severity:20}],operators:[{name:'DBA · demo',enabled:1,email_address:'demo@example.invalid'}],categories:[{name:'Database Maintenance · demo',category_id:1}],service:[{servicename:'SQL Server Agent (mock)',status_desc:'Running · 模拟'}]};
   return {identity,rows:resources[p.resource]||[],truncated:false};
  }
  throw Error('演示不支持此方法');
 }};
})();
