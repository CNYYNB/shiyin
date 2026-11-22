#!/usr/bin/env python3
"""0.2.1 suite: 「手机连电脑」(reverse mode, fake phone that ONLY makes outbound connections) and the
USB fallback (拾音数据.json copied into a music folder). Run via test/run_e2e.sh e2e_v21.py."""
import hashlib, json, os, re, shutil, socket, subprocess, sys, time, urllib.request
urllib.request.install_opener(urllib.request.build_opener(urllib.request.ProxyHandler({})))
from playwright.sync_api import sync_playwright

T = os.path.dirname(os.path.abspath(__file__))
PC = os.path.join(T, 'pc')
SHOTS = os.path.join(T, 'shots'); os.makedirs(SHOTS, exist_ok=True)
url = re.search(r'SHIYIN_URL (\S+)', open('/tmp/app.log').read()).group(1)
results = []
def check(name, ok, info=''):
    results.append((name, bool(ok), info)); print(('PASS ' if ok else 'FAIL ') + name, info, flush=True)

def port_open(port):
    s = socket.socket(); s.settimeout(1)
    try: s.connect(('127.0.0.1', port)); return True
    except OSError: return False
    finally: s.close()

def sha(p): return hashlib.sha256(open(p, 'rb').read()).hexdigest()
def key_of(fn, size):
    base = re.sub(r'\s*\(\d+\)$', '', os.path.splitext(fn)[0]).strip().lower()
    return base + '|' + str(size)

procs = []
with sync_playwright() as p:
    b = p.chromium.launch()
    pg = b.new_page(viewport={'width': 1280, 'height': 860})
    errors = []
    pg.on('pageerror', lambda e: errors.append(str(e)))
    pg.on('console', lambda m: m.type == 'error' and errors.append(m.text))
    pg.goto(url)
    pg.wait_for_selector('.row', timeout=15000); time.sleep(0.5)
    api = lambda path, method='GET', body=None: pg.evaluate('([p, m, b]) => api(p, b ? {method: m, body: b} : {method: m})', [path, method, body])
    n0 = len(api('/api/library')['tracks'])
    check('initial library 8 tracks', n0 == 8, n0)

    # ---------- 手机连电脑 ----------
    check('reverse port closed before dialog', not port_open(18522))
    pg.click('#btnPhone')
    pg.wait_for_function('() => /\\d{3} \\d{3}/.test((document.getElementById("revCode")||{}).textContent || "")', timeout=8000)
    code = pg.inner_text('#revCode').replace(' ', '')
    ips = pg.inner_text('#revIps')
    rev_txt = pg.inner_text('#phRev')
    print('code', code, 'ips', ips)
    check('PC shows 6-digit code', re.fullmatch(r'\d{6}', code), code)
    check('PC shows LAN IPs without TUN/link-local', ips and '198.18.' not in ips and '169.254.' not in ips and '127.0.0.1' not in ips, ips)
    check('firewall hint shown', '如果 Windows 弹出防火墙提示，请勾选“专用网络”' in rev_txt)
    check('reverse TCP 18522 listening while dialog open', port_open(18522))
    g = pg.inner_text('#phGuide')
    order = [g.find(x) for x in ('手机开热点，电脑连手机的热点', '用数据线拷贝', '关闭路由器的 AP 隔离', '电脑开热点')]
    check('0.2.3 guide: AP isolation order (phone hotspot → USB → router → PC hotspot 不推荐)', all(i >= 0 for i in order) and order == sorted(order) and '不推荐' in g and '死机' in g, order)
    pg.wait_for_selector('#phFound:has-text("没有找到手机")', timeout=8000)
    check('no direct phone (fake phone has no listening sockets)', not port_open(18520))
    pg.screenshot(path=f'{SHOTS}/v21_01_reverse_dialog.png')

    # wrong code → refused
    r = subprocess.run([sys.executable, f'{T}/fake_phone.py', f'{T}/phone', '--reverse', '127.0.0.1', '--discover', '--code', '000001'],
                       capture_output=True, text=True, timeout=30)
    out = r.stdout
    check('UDP discovery finds PC (SHIYIN_FIND_PC → shiyin-pc, port 18522)', 'DISCOVERED' in out and '"shiyin-pc"' in out and '"port": 18522' in out, out[:200])
    check('wrong code refused (ERR badcode)', r.returncode == 3 and 'ERR badcode' in out, (r.returncode, out[-200:]))

    # right code → UI connects automatically → library
    ph = subprocess.Popen([sys.executable, f'{T}/fake_phone.py', f'{T}/phone', '--reverse', '127.0.0.1', '--discover', '--code', code],
                          stdout=open('/tmp/revphone.log', 'w'), stderr=subprocess.STDOUT)
    procs.append(ph)
    pg.wait_for_selector('.ph-head', timeout=25000)
    info = pg.inner_text('.ph-head .info'); print(info)
    check('reverse: phone library shown automatically', 'Redmi K70E' in pg.inner_text('.modal h3'), pg.inner_text('.modal h3'))
    check('reverse: present counted', '电脑上已有 2 首' in info, info)
    pg.screenshot(path=f'{SHOTS}/v21_02_reverse_library.png')
    pg.click('#phSeg button[data-f=all]'); time.sleep(0.3)
    pg.click('#phImpAll')
    pg.wait_for_function('() => { const t = document.getElementById("phProgTxt"); return t && t.textContent.startsWith("完成"); }', timeout=90000)
    msg = pg.inner_text('#phProgTxt'); print(msg)
    check('reverse: import finished', '新导入 3 首' in msg and '已有 2 首' in msg and '失败' not in msg, msg)
    pg.screenshot(path=f'{SHOTS}/v21_03_reverse_done.png')
    for fn in ['Faye - Red Bean.m4a', 'Jay Chou - Sunny Day.flac', 'qingtian.mp3']:
        pass
    same = all(os.path.exists(os.path.join(PC, f)) and sha(os.path.join(PC, f)) == sha(os.path.join(T, 'phone', f)) for f in ['Faye - Red Bean.m4a', 'Jay Chou - Sunny Day.flac'])
    check('reverse: files byte-identical', same)
    check('reverse: .lrc saved beside song', os.path.exists(os.path.join(PC, 'Jay Chou - Sunny Day.lrc')))
    pg.click('.modal .x'); time.sleep(1)
    lib = api('/api/library')
    rows = [(t['title'], t['artist']) for t in lib['tracks']]
    check('reverse: library grew to 11', len(rows) == 11, len(rows))
    check('reverse: Chinese names applied', ('十年', '陈奕迅') in rows and ('红豆', '王菲') in rows, rows)
    pls = {pl['name']: pl for pl in lib['playlists']}
    check('reverse: playlist 跑步 (2) + 我喜欢 merged', '跑步' in pls and len(pls['跑步']['keys']) == 2 and len([pl for pl in lib['playlists'] if pl.get('fav')][0]['keys']) == 2, {k: len(v['keys']) for k, v in pls.items()})
    plog = open('/tmp/revphone.log').read()
    check('reverse: phone served over its own outbound connections (no 401)', 'library requested' in plog and '401' not in plog)
    # dialog closed + import over → listener goes away by itself (heartbeat TTL 20 s, janitor every 10 s)
    t0 = time.time()
    while time.time() - t0 < 45 and port_open(18522): time.sleep(1)
    check('reverse listener closes after the dialog is closed', not port_open(18522), round(time.time() - t0))
    ph.terminate()

    # ---------- USB: 拾音数据.json ----------
    usb = os.path.join(PC, '手机拷贝')
    os.makedirs(os.path.join(usb, '.拾音封面'), exist_ok=True)
    song = os.path.join(usb, 'USB Song.ogg'); shutil.copy(os.path.join(T, 'lib', 'vorbis.ogg'), song)
    usb_key = key_of('USB Song.ogg', os.path.getsize(song))
    eason_key = key_of('Eason - Ten Years.mp3', os.path.getsize(os.path.join(PC, 'Eason - Ten Years.mp3')))
    cover_name = hashlib.md5(usb_key.encode()).hexdigest() + '.jpg'
    shutil.copy(os.path.join(T, 'red.jpg'), os.path.join(usb, '.拾音封面', cover_name))
    doc = {'app': 'shiyin', 'kind': 'shiyin-data', 'format': 1, 'device': 'Redmi K70E', 'version': '2.2', 'updated': 1,
           'tracks': {usb_key: {'title': '优盘歌', 'artist': '优盘歌手', 'album': '优盘专辑', 'src': 'manual', 'cover': cover_name, 'coverVer': 1},
                      'missing song|123': {'title': '不在电脑上'}},
           'playlists': [{'name': '我喜欢', 'fav': True, 'keys': [usb_key]}, {'name': '优盘歌单', 'fav': False, 'keys': [usb_key, eason_key, 'missing song|123']}]}
    json.dump(doc, open(os.path.join(usb, '拾音数据.json'), 'w', encoding='utf-8'), ensure_ascii=False)
    audio_before = {f: sha(os.path.join(dp, f)) for dp, _, fs in os.walk(PC) for f in fs if not f.endswith(('.json', '.jpg', '.lrc'))}
    data_before = sha(os.path.join(usb, '拾音数据.json'))

    def rescan():
        api('/api/scan', 'POST')
        for _ in range(100):
            s = api('/api/scan')
            if not s['running']: return s
            time.sleep(0.2)
    s1 = rescan(); print('scan1', s1.get('usb'))
    check('USB: scan reports merged data', '已读取手机拾音数据' in (s1.get('usb') or '') and '1 首中文名' in s1['usb'] and '1 张封面' in s1['usb'], s1.get('usb'))
    lib = api('/api/library')
    t = [x for x in lib['tracks'] if x['key'] == usb_key]
    check('USB: name override applied', t and (t[0]['title'], t[0]['artist'], t[0]['album']) == ('优盘歌', '优盘歌手', '优盘专辑'), t[:1])
    cov = pg.evaluate('async (id) => { const r = await fetch("/api/cover?id=" + id); return Array.from(new Uint8Array(await r.arrayBuffer())).length; }', t[0]['id']) if t else 0
    check('USB: custom cover imported', cov == os.path.getsize(os.path.join(T, 'red.jpg')), cov)
    pls = {pl['name']: pl for pl in lib['playlists']}
    fav = [pl for pl in lib['playlists'] if pl.get('fav')][0]
    check('USB: playlist 优盘歌单 created by key', '优盘歌单' in pls and pls['优盘歌单']['keys'][:2] == [usb_key, eason_key], pls.get('优盘歌单'))
    check('USB: 我喜欢 merged', usb_key in fav['keys'] and len(fav['keys']) == 3, len(fav['keys']))
    s2 = rescan(); lib2 = api('/api/library')
    pls2 = {pl['name']: pl for pl in lib2['playlists']}
    check('USB: second scan is a no-op (idempotent)', not s2.get('usb') and len(pls2['优盘歌单']['keys']) == 3 and len([pl for pl in lib2['playlists'] if pl.get('fav')][0]['keys']) == 3 and len(lib2['playlists']) == len(lib['playlists']), s2.get('usb'))
    # the phone changed something → merged again, still no duplicates
    doc['tracks'][usb_key]['title'] = '优盘歌（改）'
    json.dump(doc, open(os.path.join(usb, '拾音数据.json'), 'w', encoding='utf-8'), ensure_ascii=False)
    data_before = sha(os.path.join(usb, '拾音数据.json'))
    s3 = rescan(); lib3 = api('/api/library')
    t3 = [x for x in lib3['tracks'] if x['key'] == usb_key]
    check('USB: changed file merged again', t3 and t3[0]['title'] == '优盘歌（改）' and len({pl['name']: pl for pl in lib3['playlists']}['优盘歌单']['keys']) == 3, (s3.get('usb'), t3[:1]))
    audio_after = {f: sha(os.path.join(dp, f)) for dp, _, fs in os.walk(PC) for f in fs if not f.endswith(('.json', '.jpg', '.lrc'))}
    check('USB: audio files never modified', audio_before == audio_after)
    check('USB: data file left untouched', sha(os.path.join(usb, '拾音数据.json')) == data_before)
    pg.reload(); pg.wait_for_selector('.row'); time.sleep(0.8)
    check('USB: renamed song visible in UI', '优盘歌（改）' in pg.inner_text('#main'))
    pg.screenshot(path=f'{SHOTS}/v21_04_usb.png')
    errors = [e for e in errors if '400 (Bad Request)' not in e]
    check('no JS errors', not errors, errors[:5])
    b.close()

for pr in procs:
    try: pr.terminate()
    except Exception: pass
fails = [r for r in results if not r[1]]
print(f'\n{len(results) - len(fails)}/{len(results)} passed')
json.dump(results, open(os.path.join(T, 'e2e_v21_results.json'), 'w'), ensure_ascii=False, indent=1)
sys.exit(1 if fails else 0)
