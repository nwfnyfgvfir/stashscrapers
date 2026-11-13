"""Generate the checked-in API contract from explicit public request schemas."""
import json
from pathlib import Path

schema={'type':'object','properties':{'scraper_id':{'type':'string'},'object':{'type':'string','enum':['scene','performer']},'query':{'type':'string'},'url':{'type':'string','format':'uri'},'fragment':{'type':'object','additionalProperties':True},'operation':{'enum':['fragment','query_fragment']},'rules_version':{'type':'string'}},'required':['scraper_id','object'],'additionalProperties':False}
result={'type':'object','properties':{key:{'type':['string','null']} for key in ['title','name','code','details','date','director']}}
result['properties'].update({'duration':{'type':['number','null']},'studio':{'type':['object','null']},'performers':{'type':['array','null'],'items':{'type':'object'}},'tags':{'type':['array','null'],'items':{'type':'object'}},'images':{'type':'array','items':{'type':'string'}},'urls':{'type':'array','items':{'type':'string'}},'scraper_id':{'type':'string'},'rules_version':{'type':'string'},'raw':{'type':'object'},'warnings':{'type':'array','items':{'type':'string'}}})
paths={}
def endpoint(path,method,summary,request=None,response=None):
    op={'summary':summary,'responses':{'200':{'description':'成功','content':{'application/json':{'schema':response or {'type':'object'}}}}},'security':[{'BearerAuth':[]}]}
    if request:op['requestBody']={'required':True,'content':{'application/json':{'schema':request}}}
    if '{id}' in path:op['parameters']=[{'name':'id','in':'path','required':True,'schema':{'type':'string'},'description':'稳定 ID；可包含目录斜线，需 URL 编码'}]
    for code in ['400','401','403','404','409','422','424','499','500','502','503','504']:op['responses'][code]={'description':'结构化错误','content':{'application/json':{'schema':{'$ref':'#/components/schemas/Error'}}}}
    paths.setdefault(path,{})[method]=op
for path,summary in [('/api/health','健康与资源就绪状态'),('/api/scrapers','真实规则目录及逐动作能力'),('/api/scrapers/{id}','单个规则能力'),('/api/config','读取脱敏配置'),('/api/updates','计划、状态、历史与保留版本'),('/api/dependencies','依赖与版本关系'),('/api/logs','脱敏运行事件')]:endpoint(path,'get',summary)
endpoint('/api/scrapers/{id}','patch','设置启用状态、脚本授权及独立凭据',{'type':'object','properties':{'enabled':{'type':'boolean'},'script_approved':{'type':'boolean'},'headers':{'type':'object','additionalProperties':{'type':'string'}},'cookie':{'type':'string'},'api_key':{'type':'string'},'api_key_header':{'type':'string'}},'additionalProperties':False})
response={'type':'object','properties':{'results':{'type':'array','items':{'$ref':'#/components/schemas/Result'}},'rules_version':{'type':'string'},'warnings':{'type':'array','items':{'type':'string'}}}}
endpoint('/api/search','post','名称搜索；保留所有候选',schema,response);endpoint('/api/detail','post','按 URL 或 fragment 获取详情',schema,response)
endpoint('/api/config','put','保存完整配置；脱敏占位符保留原密钥',{'type':'object','description':'完整 Config 结构，见 config.example.json；服务器严格检查字段。'})
for path,summary in [('/api/reload','重新加载配置、规则并补齐已审核 Python 依赖'),('/api/updates/check','立即检查并自动应用通过验证的新提交')]:endpoint(path,'post',summary)
endpoint('/api/install','post','安装完整上游快照或配置的本地目录',{'type':'object','properties':{'source':{'enum':['upstream','local']}},'required':['source'],'additionalProperties':False})
endpoint('/api/updates/rollback','post','回滚并临时固定版本 24 小时',{'type':'object','properties':{'version':{'type':'string','description':'留空选取最近的保留可用版本'}},'additionalProperties':False})
paths['/api/health']['get']['security']=[]
error_schema={'type':'object','properties':{'error':{'type':'object','properties':{'code':{'type':'string'},'message':{'type':'string'}}}}}
doc={
    'openapi':'3.1.0',
    'info':{'title':'拾录 · Stash 社区刮削服务','version':'0.1.0','description':'独立 Go 服务；可选 Bearer Token。规则 availability 不等于网站在线验证。'},
    'servers':[{'url':'/'}],
    'paths':paths,
    'components':{
        'securitySchemes':{'BearerAuth':{'type':'http','scheme':'bearer'}},
        'schemas':{'ScrapeRequest':schema,'Result':result,'Error':error_schema}
    }
}
Path('internal/web/openapi.json').write_text(json.dumps(doc,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
