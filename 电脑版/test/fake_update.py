#!/usr/bin/env python3
"""Fake update server + fake iTunes catalogue + fake LRCLIB for e2e_v2.py (port 18599).
/pc-version.json, /<setup>.exe (Range), /itunes/search, /art/<n>.jpg, /lrclib/api/{get,search}
/ctl?version=9.9.9&down=1&badsha=1&slow=1 changes behaviour; /stats returns request counters."""
import hashlib, json, os, random, re, time, urllib.parse
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler

T = os.path.dirname(os.path.abspath(__file__))
random.seed(7)
SETUP = bytes(random.getrandbits(8) for _ in range(3 * 1024 * 1024))
CTL = {'version': '0.2.1', 'down': '0', 'badsha': '0', 'slow': '1'}
STATS = {'version': 0, 'setup': 0, 'itunes': 0, 'art': 0, 'lrclib': 0}

ITUNES = {  # lowercase title → (trackName, artistName, collectionName) as the HK/TW store returns them
    'ten years': [('十年', '陳奕迅', '黑白灰'), ('十年 (Live)', '陳奕迅', '演唱會 2010')],
    'test flac': [('測試弗拉克', '樂隊甲', '專輯甲')],
    'song m4a': [('歌曲甲', '歌手乙', '專輯乙')],
}

def durations():
    try:
        d = json.load(open(os.path.join(T, 'data', 'library.json')))
        return {t['title'].lower(): t['duration'] for t in d['tracks']}
    except Exception:
        return {}

class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def send(self, code, body, ct='application/json', extra=None):
        if isinstance(body, (dict, list)): body = json.dumps(body, ensure_ascii=False).encode()
        if isinstance(body, str): body = body.encode()
        self.send_response(code); self.send_header('Content-Type', ct); self.send_header('Content-Length', str(len(body)))
        for k, v in (extra or {}).items(): self.send_header(k, v)
        self.end_headers(); self.wfile.write(body)
    def do_GET(self):
        u = urllib.parse.urlparse(self.path); q = dict(urllib.parse.parse_qsl(u.query)); p = u.path
        if p == '/ctl':
            CTL.update(q); return self.send(200, CTL)
        if p == '/stats': return self.send(200, STATS)
        if p == '/pc-version.json':
            STATS['version'] += 1
            if CTL['down'] == '1': return self.send(503, 'down', 'text/plain')
            v = CTL['version']
            sha = hashlib.sha256(SETUP).hexdigest()
            if CTL['badsha'] == '1': sha = '0' * 64
            return self.send(200, {'version': v, 'setup': f'shiyin-pc-setup-{v}.exe', 'size': len(SETUP), 'sha256': sha,
                                   'notes': '测试更新说明：新增主题、内置更新\n第二行说明'})
        if p.startswith('/shiyin-pc-setup-') and p.endswith('.exe'):
            STATS['setup'] += 1
            start = 0
            rg = self.headers.get('Range')
            if rg:
                start = int(re.match(r'bytes=(\d+)-', rg).group(1))
            data = SETUP[start:]
            self.send_response(206 if start else 200)
            self.send_header('Content-Type', 'application/octet-stream'); self.send_header('Content-Length', str(len(data)))
            if start: self.send_header('Content-Range', f'bytes {start}-{len(SETUP)-1}/{len(SETUP)}')
            self.end_headers()
            for i in range(0, len(data), 128 * 1024):
                self.wfile.write(data[i:i + 128 * 1024])
                if CTL['slow'] == '1': time.sleep(0.06)
            return
        if p == '/itunes/search':
            STATS['itunes'] += 1
            term = q.get('term', '').lower()
            durs = durations()
            res = []
            for k, hits in ITUNES.items():
                if k in term:
                    d = durs.get(k, 0)
                    for i, (a, b, c) in enumerate(hits):
                        res.append({'trackName': a, 'artistName': b, 'collectionName': c, 'trackTimeMillis': d + i * 1000,
                                    'artworkUrl100': f'http://127.0.0.1:18599/art/{k.replace(" ", "_")}{i}/100x100bb.jpg'})
            return self.send(200, {'resultCount': len(res), 'results': res})
        if p.startswith('/art/'):
            STATS['art'] += 1
            return self.send(200, open(os.path.join(T, 'red.jpg'), 'rb').read(), 'image/jpeg')
        if p.startswith('/lrclib/api/'):
            STATS['lrclib'] += 1
            if p.endswith('/get'): return self.send(404, {'code': 404})
            t = q.get('track_name') or q.get('q') or ''
            if 'wavfile' not in t.lower(): return self.send(200, [])
            return self.send(200, [
                {'trackName': 'wavfile', 'artistName': '测试', 'albumName': '测试专辑', 'duration': 12,
                 'plainLyrics': '手动第一行\n手动第二行', 'syncedLyrics': '[00:00.50]手动第一行\n[00:03.00]手动第二行\n[00:06.00]手动第三行'},
                {'trackName': 'wavfile (plain)', 'artistName': '测试', 'albumName': '', 'duration': 30,
                 'plainLyrics': '纯文本歌词', 'syncedLyrics': None}])
        self.send(404, 'not found', 'text/plain')

ThreadingHTTPServer(('127.0.0.1', 18599), H).serve_forever()
