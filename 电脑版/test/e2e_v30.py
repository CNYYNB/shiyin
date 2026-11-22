#!/usr/bin/env python3
"""0.3.0 now-playing bridge for 驿: GET /api/np (no token, loopback only) follows the player, POST /api/np/ctl
toggle/play/pause/next/prev runs in the page, np-port.txt written to SHIYIN_NP_DIR, browser pages refused."""
import json, os, re, time, urllib.request
urllib.request.install_opener(urllib.request.build_opener(urllib.request.ProxyHandler({})))
from playwright.sync_api import sync_playwright

url = re.search(r'SHIYIN_URL (\S+)', open('/tmp/app.log').read()).group(1)
base = url.split('/?')[0]
NPDIR = os.environ.get('SHIYIN_NP_DIR', '')
results = []
def check(name, ok, info=''):
    results.append((name, bool(ok), info)); print(('PASS ' if ok else 'FAIL ') + name, info, flush=True)
def np():
    return json.load(urllib.request.urlopen(base + '/api/np', timeout=5))
def ctl(d):
    return json.load(urllib.request.urlopen(urllib.request.Request(base + '/api/np/ctl?do=' + d, method='POST'), timeout=5))
def wait(f, t=6):
    end = time.time() + t
    while time.time() < end:
        v = f()
        if v: return v
        time.sleep(0.2)
    return f()

if NPDIR:
    pf = os.path.join(NPDIR, 'np-port.txt')
    check('np-port.txt written', os.path.exists(pf) and open(pf).read().strip() == base.rsplit(':', 1)[1], pf)

with sync_playwright() as p:
    b = p.chromium.launch(args=['--autoplay-policy=no-user-gesture-required'])
    pg = b.new_page(viewport={'width': 1280, 'height': 800})
    errors = []
    pg.on('pageerror', lambda e: errors.append(str(e)))
    pg.goto(url); pg.wait_for_selector('.row', timeout=15000); time.sleep(0.5)
    n0 = np()
    check('np before playing', n0['app'] == 'shiyin' and n0['playing'] is False and n0['ready'] is True, n0)

    pg.click('.row:has-text("wavfile")'); pg.wait_for_function('() => !document.querySelector("#audio").paused', timeout=8000)
    title = pg.evaluate('curTrack().dTitle'); artist = pg.evaluate('curTrack().dArtist'); album = pg.evaluate('curTrack().dAlbum')
    n1 = wait(lambda: (lambda n: n if n['playing'] and n['title'] == title else None)(np()))
    check('np follows the player', n1 and n1['artist'] == artist and n1['album'] == album and n1['dur'] > 0, n1)
    time.sleep(1.2)
    n2 = np(); check('pos advances', n2['pos'] > n1['pos'], (n1['pos'], n2['pos']))

    ctl('pause'); check('ctl pause', wait(lambda: pg.evaluate('audio.paused')))
    check('np paused', wait(lambda: np()['playing'] is False))
    ctl('pause'); time.sleep(0.5); check('pause is idempotent', pg.evaluate('audio.paused'))
    ctl('play'); check('ctl play', wait(lambda: not pg.evaluate('audio.paused')))
    ctl('play'); time.sleep(0.5); check('play is idempotent', not pg.evaluate('audio.paused'))
    ctl('toggle'); check('ctl toggle → paused', wait(lambda: pg.evaluate('audio.paused')))
    ctl('toggle'); check('ctl toggle → playing', wait(lambda: not pg.evaluate('audio.paused')))
    qi = pg.evaluate('S.qi')
    ctl('next'); check('ctl next', wait(lambda: pg.evaluate('S.qi') != qi))
    t2 = pg.evaluate('curTrack().dTitle')
    check('np has the next song', wait(lambda: np()['title'] == t2), t2)
    pg.evaluate('audio.currentTime = 0')
    ctl('prev'); check('ctl prev', wait(lambda: pg.evaluate('S.qi') == qi))

    # a web page in a browser cannot read or drive it
    other = b.new_page(); other.goto('about:blank')
    r = other.evaluate('''async (u) => { try { const r = await fetch(u + '/api/np/ctl?do=pause', {method: 'POST', mode: 'no-cors'}); return 'sent'; } catch (e) { return 'err'; } }''', base)
    time.sleep(0.6)
    check('cross-origin page cannot pause', not pg.evaluate('audio.paused'), r)
    r = other.evaluate('''async (u) => { try { const r = await fetch(u + '/api/np'); return await r.text(); } catch (e) { return 'blocked'; } }''', base)
    check('cross-origin page cannot read', 'shiyin' not in r, r[:80])
    check('desk lyrics still off', pg.evaluate('!deskOn()'))
    check('no page errors', not errors, errors)
    b.close()

ok = sum(1 for r in results if r[1])
print(f'{ok}/{len(results)} passed')
json.dump(results, open(os.path.join(os.path.dirname(os.path.abspath(__file__)), 'e2e_v30_results.json'), 'w'), ensure_ascii=False, indent=1, default=str)
raise SystemExit(0 if ok == len(results) else 1)
