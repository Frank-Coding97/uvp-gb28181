"""Generate default guest grants; no database connection. Run with --check to detect drift."""
import argparse
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DB = ROOT / 'server/resource/database'
VERSION = '2026-09-05-guest-readonly-permissions'
START, END = '-- guest-readonly:start', '-- guest-readonly:end'


def generate(c, dialect):
    def q(s):
        return ('N' if dialect == 'sqlserver' else '') + "'" + str(s).replace("'", "''") + "'"
    role = '(SELECT MIN(id) FROM sys_role WHERE name=' + q(c['roleName']) + ' AND deleted_at IS NULL)'
    subject = "CONCAT('role_'," + role + ')'
    select_menus = '(m.type=2 AND m.path IN (' + ','.join(map(q, c['pages'])) + ')) OR (m.type=3 AND m.permission IN (' + ','.join(map(q, c['permissions'])) + '))'
    selected = 'm.deleted_at IS NULL AND m.disable=0 AND (' + select_menus + ')'
    allowed = ' OR '.join('(v1=' + q(a['path']) + ' AND v2=' + q(a['method']) + ')' for a in c['allowedAPIs'])
    out = [START, '-- Generated from guest-permissions.json. Configure guest only; keep other roles and user assignments.']
    for button in c.get('newButtons', []):
        out.append('INSERT INTO sys_menu(parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) SELECT m.id,' + ','.join([q(''),q('Permission_'+button['permission'].replace(':','_')),q(''),q(button['title']),'1','0','100','3',q(button['permission']),q(''),'CURRENT_TIMESTAMP','CURRENT_TIMESTAMP','1']) + ' FROM sys_menu m WHERE m.path='+q(button['parentPath'])+' AND m.type=2 AND m.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu x WHERE x.permission='+q(button['permission'])+' AND x.deleted_at IS NULL)')
    old, new = '/api/gb28181/play/:deviceId/probe', '/api/gb28181/stream-probes/:streamId'
    # Existing deployments may already contain either API path. Merge links without changing menu IDs.
    out += [
        'INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '+','.join([q('视频探针诊断'),q(new),q('POST'),q('按钮权限目录'),'CURRENT_TIMESTAMP','CURRENT_TIMESTAMP','1'])+' WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='+q(new)+" AND method='POST' AND deleted_at IS NULL)",
        'INSERT INTO sys_menu_api(menu_id,api_id) SELECT DISTINCT ma.menu_id,n.id FROM sys_menu_api ma JOIN sys_api o ON o.id=ma.api_id CROSS JOIN sys_api n WHERE o.path='+q(old)+" AND o.method='POST' AND n.path="+q(new)+" AND n.method='POST' AND n.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=ma.menu_id AND x.api_id=n.id)",
        'DELETE FROM sys_menu_api WHERE api_id IN(SELECT id FROM sys_api WHERE path='+q(old)+" AND method='POST')",
        'UPDATE sys_api SET deleted_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE path='+q(old)+" AND method='POST' AND deleted_at IS NULL",
        'INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT DISTINCT p.ptype,p.v0,'+q(new)+',p.v2,p.v3,p.v4,p.v5 FROM (SELECT DISTINCT ptype,v0,v2,v3,v4,v5 FROM sys_casbin_rule WHERE ptype=\'p\' AND v1='+q(old)+" AND v2='POST') p WHERE NOT EXISTS(SELECT 1 FROM sys_casbin_rule n WHERE n.ptype=p.ptype AND n.v0=p.v0 AND n.v1="+q(new)+' AND n.v2=p.v2 AND n.v3=p.v3)',
        'DELETE FROM sys_casbin_rule WHERE ptype=\'p\' AND v1='+q(old)+" AND v2='POST'",
    ]
    # Preserve pre-existing cloud viewers' explicit download entitlement, excluding the role being configured.
    out.append('INSERT INTO sys_role_menu(role_id,menu_id) SELECT DISTINCT r.role_id,d.id FROM sys_role_menu r JOIN sys_menu v ON v.id=r.menu_id CROSS JOIN sys_menu d JOIN sys_role sr ON sr.id=r.role_id WHERE v.permission='+q('gb28181:recording:view')+' AND v.deleted_at IS NULL AND d.permission='+q('gb28181:recording:download')+' AND d.deleted_at IS NULL AND sr.name<>'+q(c['roleName'])+' AND EXISTS(SELECT 1 FROM sys_menu_api ma JOIN sys_api a ON a.id=ma.api_id WHERE ma.menu_id=v.id AND a.path='+q('/api/gb28181/cloud-recordings/files/:id/downloads')+") AND NOT EXISTS(SELECT 1 FROM sys_role_menu x WHERE x.role_id=r.role_id AND x.menu_id=d.id)")
    out.append('DELETE FROM sys_menu_api WHERE menu_id IN(SELECT id FROM sys_menu WHERE permission='+q('gb28181:recording:view')+') AND api_id IN(SELECT id FROM sys_api WHERE (path='+q('/api/gb28181/cloud-recordings/files/:id/downloads')+" AND method='POST') OR (path="+q('/api/gb28181/cloud-recordings/downloads/:taskId')+" AND method IN('GET','DELETE')))")
    for binding in c['bindings']:
        menu = ('m.path='+q(binding['menuPath'])+' AND m.type=2') if 'menuPath' in binding else ('m.permission='+q(binding['permission'])+' AND m.type=3')
        for a in binding['apis']:
            out.append('INSERT INTO sys_api(title,path,method,api_group,created_at,updated_at,created_by) SELECT '+','.join([q('查看与观看依赖'),q(a['path']),q(a['method']),q('游客权限依赖'),'CURRENT_TIMESTAMP','CURRENT_TIMESTAMP','1'])+' WHERE NOT EXISTS(SELECT 1 FROM sys_api WHERE path='+q(a['path'])+' AND method='+q(a['method'])+' AND deleted_at IS NULL)')
            out.append('INSERT INTO sys_menu_api(menu_id,api_id) SELECT m.id,a.id FROM sys_menu m CROSS JOIN sys_api a WHERE '+menu+' AND m.deleted_at IS NULL AND a.path='+q(a['path'])+' AND a.method='+q(a['method'])+' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_menu_api x WHERE x.menu_id=m.id AND x.api_id=a.id)')
    out += [
        'INSERT INTO sys_role(name,sort,status,description,parent_id,data_scope,checked_depts,created_at,updated_at,created_by) SELECT '+','.join([q(c['roleName']),'100','1',q(c['description']),'0','4',q(''),'CURRENT_TIMESTAMP','CURRENT_TIMESTAMP','1'])+' WHERE NOT EXISTS(SELECT 1 FROM sys_role WHERE name='+q(c['roleName'])+' AND deleted_at IS NULL)',
        'UPDATE sys_role SET description='+q(c['description'])+',parent_id=0,status=1,updated_at=CURRENT_TIMESTAMP WHERE name='+q(c['roleName'])+' AND deleted_at IS NULL AND (COALESCE(description,\'\')<>'+q(c['description'])+' OR parent_id<>0 OR status<>1)',
        'DELETE FROM sys_role_menu WHERE role_id='+role+' AND menu_id NOT IN(SELECT m.id FROM sys_menu m WHERE '+selected+')',
        'INSERT INTO sys_role_menu(role_id,menu_id) SELECT '+role+',m.id FROM sys_menu m WHERE '+selected+' AND NOT EXISTS(SELECT 1 FROM sys_role_menu x WHERE x.role_id='+role+' AND x.menu_id=m.id)',
        'DELETE FROM sys_casbin_rule WHERE v0='+subject+" AND ((ptype='p' AND (v3<>'*' OR NOT ("+allowed+"))) OR ptype='g')",
        'INSERT INTO sys_casbin_rule(ptype,v0,v1,v2,v3,v4,v5) SELECT DISTINCT \'p\','+subject+',a.path,a.method,\'*\',\'\',\'\' FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id JOIN sys_menu_api ma ON ma.menu_id=m.id JOIN sys_api a ON a.id=ma.api_id WHERE rm.role_id='+role+' AND m.deleted_at IS NULL AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sys_casbin_rule p WHERE p.ptype=\'p\' AND p.v0='+subject+" AND p.v1=a.path AND p.v2=a.method AND p.v3='*')",
    ]
    return '\n\n'.join(s+';' if not s.startswith('--') else s for s in out)+'\n'+END+'\n'


def outputs(c):
    for dialect,suffix,name in [('mysql','','uvp-gb28181.sql'),('postgresql','-postgresql','postgresql_converted.sql'),('sqlserver','-sqlserver','sqlserver_converted.sql')]:
        sql=generate(c,dialect)
        yield DB/'gb28181/migrations'/(VERSION+suffix+'.sql'),sql
        yield DB/'gb28181/migrations'/(VERSION+suffix+'-down.sql'),'-- Forward-only security correction; never restore mixed download grants or overlapping probe routes.\nSELECT 1;\n'
        p=DB/name;body=p.read_text()
        if START in body:
            body=body[:body.index(START)]+sql.rstrip()+body[body.index(END)+len(END):]
        else: body=body.rstrip()+'\n\n'+sql
        yield p,body


def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--check',action='store_true');args=parser.parse_args()
    c=json.loads((DB/'gb28181/guest-permissions.json').read_text());stale=[]
    for p,body in outputs(c):
        if args.check:
            if not p.exists() or p.read_text()!=body: stale.append(str(p))
        else:p.write_text(body)
    if stale:raise SystemExit('Guest outputs drift: '+', '.join(stale))
    print('Guest outputs match' if args.check else 'Guest outputs generated')

if __name__=='__main__':main()
