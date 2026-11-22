#!/usr/bin/env python3
"""Fake 拾音 Android phone implementing shiyin-sync-protocol v1 (for testing the PC client).

Usage: fake_phone.py <music_dir> [--code 123456] [--http-port 18520] [--udp-port 18521]
       [--library-delay 3] [--fail-once KEY_SUBSTR] [--cut-once KEY_SUBSTR]
--fail-once: first /api/file for a matching key returns 500 (tests retry)
--cut-once : first /api/file for a matching key closes the connection halfway (tests .part resume)
--reverse HOST[:PORT] : 「手机连电脑」 mode (protocol 1.1): NO listening sockets at all; the phone opens
             outbound connections to the PC (handshake with --code = the code shown on the PC) and serves
             the PC's HTTP requests on them. --discover first finds the PC via UDP 18523 (SHIYIN_FIND_PC).
"""
import argparse, json, os, re, socket, struct, threading, time, urllib.parse
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler

ap = argparse.ArgumentParser()
ap.add_argument('dir')
ap.add_argument('--code', default='123456')
ap.add_argument('--http-port', type=int, default=18520)
ap.add_argument('--udp-port', type=int, default=18521)
ap.add_argument('--library-delay', type=float, default=0)
ap.add_argument('--fail-once', default='')
ap.add_argument('--cut-once', default='')
ap.add_argument('--name', default='Redmi K70E')
ap.add_argument('--reverse', default='')
ap.add_argument('--discover', action='store_true')
ap.add_argument('--slots', type=int, default=4)
args = ap.parse_args()

LOG = []
def log(*a):
    s = ' '.join(str(x) for x in a)
    LOG.append(s); print('[phone]', s, flush=True)

def key_of(file_name, size):
    base = os.path.splitext(file_name)[0]
    base = re.sub(r' \(\d+\)$', '', base)
    return base.strip().lower() + '|' + str(size)

# Chinese display names the phone "applied"
DISPLAY = {
    'jay chou - sunny day': ('晴天', '周杰伦', '叶惠美'),
    'eason - ten years': ('十年', '陈奕迅', '黑白灰'),
    'faye - red bean': ('红豆', '王菲', '唱游'),
    'qingtian': ('晴天', '周杰伦', '叶惠美'),
    'clash': ('冲突测试', '测试歌手', '测试专辑'),
}
LRC = {'jay chou - sunny day': '[00:00.50]故事的小黄花\n[00:02.00]從出生那年就飄著\n[00:04.00]童年的盪鞦韆\n'}
COVER = open(os.path.join(os.path.dirname(__file__), 'blue.jpg'), 'rb').read()

tracks = []
for fn in sorted(os.listdir(args.dir)):
    p = os.path.join(args.dir, fn)
    if not os.path.isfile(p) or fn.endswith('.lrc'):
        continue
    size = os.path.getsize(p)
    base = os.path.splitext(fn)[0].lower()
    t, a, al = DISPLAY.get(base, (os.path.splitext(fn)[0], '未知歌手', ''))
    raw = os.path.splitext(fn)[0].split(' - ')
    tracks.append({
        'key': key_of(fn, size), 'title': t, 'artist': a, 'album': al,
        'rawTitle': raw[-1], 'rawArtist': raw[0] if len(raw) > 1 else '',
        'duration': 15000, 'size': size, 'fileName': fn, 'ext': os.path.splitext(fn)[1][1:].lower(),
        'trackNo': 0, 'dateAdded': int(time.time()), 'hasLrc': base in LRC, 'lrcSynced': base in LRC,
        'hasCover': base != 'clash', 'coverVer': 0, '_path': p, '_base': base,
    })
by_key = {t['key']: t for t in tracks}
playlists = [
    {'name': '我喜欢', 'fav': True, 'keys': [t['key'] for t in tracks if t['_base'] in ('jay chou - sunny day', 'faye - red bean')]},
    {'name': '跑步', 'fav': False, 'keys': [t['key'] for t in tracks if t['_base'] in ('eason - ten years', 'qingtian')]},
]
fail_done, cut_done = set(), set()

class H(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'
    def log_message(self, *a): pass
    def send(self, code, body=b'', ctype='application/json', extra=None):
        self.send_response(code)
        self.send_header('Access-Control-Allow-Origin', '*')
        self.send_header('Content-Type', ctype)
        self.send_header('Content-Length', str(len(body)))
        for k, v in (extra or {}).items(): self.send_header(k, v)
        self.end_headers()
        if self.command != 'HEAD': self.wfile.write(body)
    def j(self, o, code=200): self.send(code, json.dumps(o, ensure_ascii=False).encode(), 'application/json; charset=utf-8')
    def do_GET(self):
        u = urllib.parse.urlparse(self.path); q = urllib.parse.parse_qs(u.query)
        if u.path == '/api/ping':
            return self.j({'app': 'shiyin', 'version': '2.1', 'name': args.name, 'needCode': True})
        code = (q.get('code') or [''])[0] or self.headers.get('X-Shiyin-Code', '')
        if code != args.code and not args.reverse:
            log('401', u.path); return self.j({'error': 'bad code'}, 401)
        key = (q.get('key') or [''])[0]
        if u.path == '/api/library':
            log('library requested (header code=%s)' % self.headers.get('X-Shiyin-Code'))
            time.sleep(args.library_delay)
            return self.j({'app': 'shiyin', 'version': '2.1', 'name': args.name,
                           'tracks': [{k: v for k, v in t.items() if not k.startswith('_')} for t in tracks], 'playlists': playlists})
        t = by_key.get(key)
        if u.path == '/api/lrc':
            if not t or t['_base'] not in LRC: return self.j({}, 404)
            log('lrc', key); return self.send(200, LRC[t['_base']].encode(), 'text/plain; charset=utf-8')
        if u.path == '/api/cover':
            if not t or not t['hasCover']: return self.j({}, 404)
            return self.send(200, COVER, 'image/jpeg')
        if u.path == '/api/file':
            if not t: return self.j({'error': '没有这首歌'}, 404)
            if args.fail_once and args.fail_once in key and key not in fail_done:
                fail_done.add(key); log('FAIL once', key); return self.j({'error': 'boom'}, 500)
            data = open(t['_path'], 'rb').read()
            size = len(data); start, end = 0, size - 1; code = 200
            rng = self.headers.get('Range')
            if rng:
                m = re.match(r'bytes=(\d*)-(\d*)', rng)
                if m.group(1): start = int(m.group(1))
                if m.group(2): end = int(m.group(2))
                if start >= size: return self.send(416, b'', 'text/plain', {'Content-Range': 'bytes */%d' % size})
                code = 206
            log('file', key, 'range', rng, '->', code)
            body = data[start:end + 1]
            self.send_response(code)
            self.send_header('Access-Control-Allow-Origin', '*')
            self.send_header('Content-Type', 'audio/' + t['ext'])
            self.send_header('Content-Length', str(len(body)))
            self.send_header('Accept-Ranges', 'bytes')
            if code == 206: self.send_header('Content-Range', 'bytes %d-%d/%d' % (start, end, size))
            self.send_header('Content-Disposition', "attachment; filename*=UTF-8''" + urllib.parse.quote(t['fileName']))
            self.end_headers()
            if args.cut_once and args.cut_once in key and key not in cut_done:
                cut_done.add(key); half = len(body) // 2
                self.wfile.write(body[:half]); self.wfile.flush(); log('CUT after', half)
                self.close_connection = True
                try: self.connection.shutdown(socket.SHUT_RDWR)
                except Exception: pass
                return
            self.wfile.write(body)
            return
        self.j({'error': 'not found'}, 404)
    do_HEAD = do_GET

def udp():
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.setsockopt(socket.SOL_SOCKET, socket.SO_BROADCAST, 1)
    s.bind(('0.0.0.0', args.udp_port))
    while True:
        data, addr = s.recvfrom(2048)
        log('udp', data, 'from', addr)
        if data.strip() == b'SHIYIN_DISCOVER':
            reply = {'app': 'shiyin', 'name': args.name, 'ip': '127.0.0.1', 'port': args.http_port, 'version': '2.1', 'count': len(tracks)}
            s.sendto(json.dumps(reply, ensure_ascii=False).encode(), addr)

import io, sys

class _Chain(io.RawIOBase):
    def __init__(self, first, rest): self.first, self.rest = io.BytesIO(first), rest
    def readable(self): return True
    def readinto(self, b):
        n = self.first.readinto(b)
        if n: return n
        data = self.rest.read1(len(b)) if hasattr(self.rest, 'read1') else self.rest.read(len(b))
        b[:len(data)] = data; return len(data)

class RevH(H):
    # skips the PC's blank keep-alive lines before the request line
    def setup(self):
        super().setup()
        while True:
            line = self.rfile.readline(65537)
            if not line: break
            if line.strip(): break
        self.rfile = io.BufferedReader(_Chain(line, self.rfile))

STATS = {'served': 0, 'handshakes': 0}
def reverse_slot(host, port, n):
    fails = 0
    while True:
        try:
            s = socket.create_connection((host, port), timeout=5)
        except OSError as e:
            fails += 1
            if fails > 40: log('slot', n, 'giving up'); return
            time.sleep(0.5); continue
        s.settimeout(45)
        hello = json.dumps({'app': 'shiyin', 'name': args.name, 'version': '2.2', 'count': len(tracks)}, ensure_ascii=False)
        s.sendall(('SHIYIN-REVERSE/1 %s %s\n' % (args.code, hello)).encode())
        line = b''
        while not line.endswith(b'\n'):
            c = s.recv(1)
            if not c: break
            line += c
        line = line.decode().strip()
        if not line.startswith('OK'):
            log('handshake refused:', line); s.close()
            if line.startswith('ERR'): print('HANDSHAKE_ERR', line, flush=True); os._exit(3)
            time.sleep(0.5); continue
        STATS['handshakes'] += 1; fails = 0
        try:
            RevH(s, (host, port), None)
            STATS['served'] += 1
        except Exception as e:
            log('slot', n, 'error', e)
        finally:
            try: s.close()
            except OSError: pass

if args.reverse:
    host, _, port = args.reverse.partition(':')
    port = int(port or 18522)
    if args.discover:
        u = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); u.settimeout(2.5)
        u.setsockopt(socket.SOL_SOCKET, socket.SO_BROADCAST, 1)
        u.sendto(b'SHIYIN_FIND_PC', (host, 18523))
        try:
            data, addr = u.recvfrom(2048); info = json.loads(data)
            print('DISCOVERED', json.dumps(info, ensure_ascii=False), flush=True)
            port = int(info.get('port') or port)
        except socket.timeout:
            print('DISCOVER_TIMEOUT', flush=True); sys.exit(2)
    log('reverse mode →', host, port, 'slots', args.slots)
    for n in range(args.slots):
        threading.Thread(target=reverse_slot, args=(host, port, n), daemon=True).start()
    while True: time.sleep(3600)
else:
    threading.Thread(target=udp, daemon=True).start()
    log('serving', len(tracks), 'tracks on', args.http_port)
    ThreadingHTTPServer(('0.0.0.0', args.http_port), H).serve_forever()
