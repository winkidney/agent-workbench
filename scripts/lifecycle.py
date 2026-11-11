#!/usr/bin/env python3
# Opt-in systemd acceptance. Stops ONLY agent-workbench.service; no real inference.
import argparse,json,os,pathlib,subprocess,time,urllib.request,urllib.error,sqlite3
p=argparse.ArgumentParser();p.add_argument('--cycles',type=int,default=100);args=p.parse_args()
repo=pathlib.Path(__file__).resolve().parents[1];binary=repo/'bin/agent-workbench';state=pathlib.Path('/tmp/awb-lifecycle-state');fixtures=pathlib.Path('/tmp/awb-lifecycle-bin');fixtures.mkdir(exist_ok=True);state.mkdir(exist_ok=True)
native=state/'codex';native.mkdir(exist_ok=True)
db=sqlite3.connect(native/'state_5.sqlite');db.execute('CREATE TABLE IF NOT EXISTS threads(id TEXT,cwd TEXT,name TEXT,title TEXT,updated_at INTEGER,source TEXT,rollout_path TEXT,archived INTEGER)');db.execute('DELETE FROM threads');db.execute('INSERT INTO threads VALUES(?,?,?,?,?,?,?,?)',('mock-thread',str(fixtures),'fixture','fixture',int(time.time()),'cli','',0));db.commit();db.close()
mock=fixtures/'codex';mock.write_text('''#!/usr/bin/python3
import sys,json,subprocess
for line in sys.stdin:
 v=json.loads(line);method=v.get('method');params=v.get('params',{})
 if method=='initialize':out={'id':v['id'],'result':{}}
 elif method in ('thread/start','thread/resume'):out={'id':v['id'],'result':{'thread':{'id':params.get('threadId','mock-thread')}}}
 elif method=='turn/start':
  child=subprocess.Popen(['/usr/bin/sleep','120']) if params.get('input',[{}])[0].get('text')=='spawn' else None
  print(json.dumps({'method':'turn/started','params':{'turn':{'id':'turn'}}}),flush=True)
  print(json.dumps({'method':'item/agentMessage/delta','params':{'delta':str(child.pid) if child else 'initialized'}}),flush=True)
  out={'method':'turn/completed','params':{}}
 else:continue
 print(json.dumps(out),flush=True)
''');mock.chmod(0o700)
def stop():subprocess.run([str(binary),'stop'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
def start():
 env=os.environ.copy();env['PATH']=str(fixtures)+':'+env['PATH'];env['CODEX_HOME']=str(native);env['CLAUDE_CONFIG_DIR']=str(state/'claude');subprocess.run([str(binary),'start','--data-dir',str(state),'--listen','127.0.0.1:17881'],env=env,check=True,stdout=subprocess.DEVNULL);time.sleep(.3)
stop();start();cookie='';host='agent.localhost:17881'
def api(path,body=None):
 global cookie
 req=urllib.request.Request('http://127.0.0.1:17881/api/'+path,data=json.dumps(body).encode() if body is not None else None,headers={'Host':host,'Origin':'http://'+host,'Content-Type':'application/json','Cookie':cookie})
 with urllib.request.urlopen(req,timeout=40) as r:
  if r.headers.get('Set-Cookie'):cookie=r.headers['Set-Cookie'].split(';')[0]
  return json.load(r)
def login():
 if api('auth/status')['pairingRequired']:api('login',{'code':(state/'pairing-code').read_text()})
def wait(sid):
 for i in range(200):
  s=next(x for x in api('sessions') if x['id']==sid)
  if s['status']=='idle':return
  if s['status']=='error':raise RuntimeError('mock engine failed')
  time.sleep(.02)
 raise RuntimeError('timeout')
def metrics():
 pid=int(subprocess.check_output(['systemctl','--user','show','agent-workbench.service','--property=MainPID','--value']))
 values={k:int(v.split()[0]) for k,v in (line.split(':',1) for line in pathlib.Path(f'/proc/{pid}/status').read_text().splitlines() if line.startswith(('VmRSS:','VmHWM:')))}
 values['fds']=len(list(pathlib.Path(f'/proc/{pid}/fd').iterdir()));return values
try:
 login();session=api('sessions',{'agent':'codex','cwd':str(fixtures),'title':'lifecycle fixture','initialMessage':'initialize'});sid=session['id'];wait(sid);initial=metrics()
 for i in range(args.cycles):
  api(f'sessions/{sid}/resume',{});wait(sid);api(f'sessions/{sid}/sleep',{});api(f'sessions/{sid}/resume',{});wait(sid);api(f'sessions/{sid}/stop',{})
  if (i+1)%20==0:print(json.dumps({'cycles':i+1,'metrics':metrics()}),flush=True)
 steady=metrics()
 api(f'sessions/{sid}/send',{'text':'spawn'});wait(sid)
 try:api(f'sessions/{sid}/sleep',{});raise RuntimeError('slept with active child')
 except urllib.error.HTTPError as e:
  if e.code!=400:raise
 api(f'sessions/{sid}/stop',{})
 api(f'sessions/{sid}/resume',{});wait(sid);api('previews',{'session':sid,'kind':'service','command':'python3 -m http.server 17999 --bind 127.0.0.1','port':17999})
 subprocess.run(['systemctl','--user','kill','--signal=SIGKILL','agent-workbench.service'],check=True);time.sleep(1)
 units=subprocess.check_output(['systemctl','--user','list-units','--state=active','--no-legend','agent-workbench-session-*.service','agent-workbench-preview-*.service'],text=True)
 if units.strip():raise RuntimeError('orphan units remain: '+units)
 start();cookie='';login();recovered=next(x for x in api('sessions') if x['id']==sid)
 if recovered['status']!='sleeping' or recovered['nativeId']!='mock-thread':raise RuntimeError('crash recovery failed')
 final=metrics();result={'cycles':args.cycles,'before':initial,'afterCycles':steady,'afterRestart':final,'activeChildSleepGuard':'passed','parentCrashCleanup':'passed','recovery':'passed'};print(json.dumps(result),flush=True);(repo/'artifacts/lifecycle.json').write_text(json.dumps(result,indent=2))
finally:stop()
