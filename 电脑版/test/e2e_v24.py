#!/usr/bin/env python3
"""0.2.4 桌面歌词: 「词」 button, prefs in prefs.json, desk page (web/desk.html) follows the main player
(same lyrics/offset/t2s), toolbar commands (prev/play/next, size, color, lock, close), no-lyrics fallback.
Also renders the previews into $DESK_PREVIEW (default /workspace/work/shiyin/preview-desklyrics)."""
import json, os, re, sys, time, urllib.request
urllib.request.install_opener(urllib.request.build_opener(urllib.request.ProxyHandler({})))
from playwright.sync_api import sync_playwright
from PIL import Image, ImageDraw, ImageFilter

T = os.path.dirname(os.path.abspath(__file__))
DATA = os.path.join(T, 'data')
PREV = os.environ.get('DESK_PREVIEW', '/workspace/work/shiyin/preview-desklyrics'); os.makedirs(PREV, exist_ok=True)
url = re.search(r'SHIYIN_URL (\S+)', open('/tmp/app.log').read()).group(1)
base = url.split('/?')[0]
results = []
def check(name, ok, info=''):
    results.append((name, bool(ok), info)); print(('PASS ' if ok else 'FAIL ') + name, info, flush=True)
def desk_prefs(): return json.load(open(os.path.join(DATA, 'prefs.json'))).get('deskLyrics', {})
def height(size): return 36 + 2 * ((size * 13 + 9) // 10) + 14

def wallpaper(w, h):
    """a typical photo-like wallpaper: sky gradient, sun glow, mountains, lake"""
    im = Image.new('RGB', (w, h))
    d = ImageDraw.Draw(im)
    for y in range(h):
        k = y / h
        d.line([(0, y), (w, y)], fill=(int(40 + 180 * k), int(90 + 100 * k), int(170 + 40 * k)))
    glow = Image.new('L', (w, h), 0); ImageDraw.Draw(glow).ellipse([w * .62, h * .18, w * .82, h * .42], fill=255)
    im.paste((255, 236, 200), mask=glow.filter(ImageFilter.GaussianBlur(60)))
    d.polygon([(0, h * .72), (w * .18, h * .45), (w * .33, h * .62), (w * .52, h * .38), (w * .7, h * .6), (w * .86, h * .5), (w, h * .66), (w, h), (0, h)], fill=(52, 70, 92))
    d.polygon([(0, h * .8), (w * .25, h * .62), (w * .45, h * .76), (w * .66, h * .6), (w, h * .78), (w, h), (0, h)], fill=(34, 52, 60))
    d.rectangle([0, h * .86, w, h], fill=(70, 120, 150))
    return im

def light_wallpaper(w, h):
    im = Image.new('RGB', (w, h)); d = ImageDraw.Draw(im)
    for y in range(h):
        k = y / h; d.line([(0, y), (w, y)], fill=(int(250 - 20 * k), int(246 - 25 * k), int(240 - 30 * k)))
    for i in range(7):
        d.ellipse([w * (i / 7) - 80, h * .55 + (i % 3) * 30, w * (i / 7) + 160, h * .55 + (i % 3) * 30 + 240], fill=(236, 222, 205))
    return im

def composite(png, bg, x, y, out):
    fg = Image.open(png).convert('RGBA'); im = bg.convert('RGBA')
    im.alpha_composite(fg, (x, y)); im.convert('RGB').save(out)

DEMO = [{'t': 0, 'text': '风吹过旧街口的梧桐'}, {'t': 4, 'text': '你说晚一点再回头'}, {'t': 8, 'text': '我把整个夏天都写进歌里'}, {'t': 12, 'text': '等你哼到最后一句'}, {'t': 16, 'text': '灯火慢慢亮起'}]

with sync_playwright() as p:
    b = p.chromium.launch(args=['--autoplay-policy=no-user-gesture-required'])
    ctx = b.new_context(viewport={'width': 1280, 'height': 800})
    pg = ctx.new_page()
    errors = []
    pg.on('pageerror', lambda e: errors.append(str(e)))
    pg.goto(url); pg.wait_for_selector('.row', timeout=15000); time.sleep(0.5)
    S = lambda e: pg.evaluate(e.replace('curIdx()', '(() => { const t = audio.currentTime + 0.25 + lyrOffset() / 1000; let i = -1; S.lyr.forEach((l, k) => { if (l.t <= t) i = k; }); return i; })()'))

    check('词 button in player bar', pg.is_visible('#btnDesk') and pg.inner_text('#btnDesk') == '词')
    check('off by default', not S('document.querySelector("#btnDesk").classList.contains("on")'))
    pg.click('.row:has-text("wavfile")'); pg.wait_for_function('() => !document.querySelector("#audio").paused', timeout=8000)
    pg.wait_for_function('() => typeof S !== "undefined" && S.lyr && S.lyr.length', timeout=10000)
    pg.click('#btnDesk'); time.sleep(0.6)
    check('click 词 → on (highlighted)', S('document.querySelector("#btnDesk").classList.contains("on")'))
    check('on saved in prefs.json deskLyrics', desk_prefs().get('on') is True, desk_prefs())
    check('window told to show (desk apply on=true)', 'desk apply on=true' in open('/tmp/app.log').read())

    dk = ctx.new_page()
    dk.set_viewport_size({'width': 880, 'height': height(36)})
    dk.on('pageerror', lambda e: errors.append('desk: ' + str(e)))
    dk.goto(base + '/desk.html'); time.sleep(1.2)
    D = lambda e: dk.evaluate(e)
    main_line = lambda: S('(S.lyr && S.lyr[curIdx()] || {}).text || ""')
    desk_cur = lambda: D('(document.querySelector(".row.cur .ln .b")||{}).textContent || ""')
    dk.wait_for_function('() => document.querySelector(".row.cur .ln")', timeout=5000)
    pg.evaluate('audio.pause()'); time.sleep(0.6)
    check('desk shows the main window\'s current line', desk_cur() == main_line() and desk_cur() != '', [desk_cur(), main_line()])
    check('desk lines == main parsed lyrics (same t2s/parse)', D('D.now.lines.map(l=>l.text).join("|")') == S('S.lyr.map(l=>l.text).join("|")'))
    check('page transparent (no body background)', D('getComputedStyle(document.body).backgroundColor') in ('rgba(0, 0, 0, 0)', 'transparent'))
    dk.evaluate('document.dispatchEvent(new MouseEvent("mouseleave"))'); time.sleep(0.4)
    check('toolbar hidden when not hovered', D('getComputedStyle(document.querySelector("#tb")).opacity') == '0')
    # offset from the main window reaches the desk page
    pg.evaluate('audio.play()'); time.sleep(0.3)
    pg.evaluate('shiftOffset(500)'); time.sleep(0.5)
    check('lyric offset follows the main window', D('D.now.offset') == 500, D('D.now.offset'))
    pg.evaluate('shiftOffset(null)'); time.sleep(0.3)
    # hover + toolbar
    dk.mouse.move(440, 80); time.sleep(0.3)
    check('hover shows toolbar', D('getComputedStyle(document.querySelector("#tb")).opacity') == '1')
    dk.click('#tb [data-c=toggle]'); time.sleep(0.8)
    check('toolbar 播放/暂停 pauses the main player', S('document.querySelector("#audio").paused'))
    check('desk knows it is paused', D('D.now.playing') is False)
    dk.click('#tb [data-c=toggle]'); time.sleep(0.8)
    check('toolbar 播放/暂停 resumes', not S('document.querySelector("#audio").paused'))
    t0 = S('curTrack().id')
    dk.click('#tb [data-c=next]'); time.sleep(1.0)
    check('toolbar 下一首 → main switches song', S('curTrack().id') != t0)
    title = S('curTrack().dTitle')
    pg.wait_for_function('() => typeof S !== "undefined" && S.lyrTrack === curTrack().id && document.querySelector("#lyrics .msg, #lyrics p")', timeout=15000); time.sleep(1.0)
    if not S('S.lyr && S.lyr.length'):
        check('no lyrics → song title + 拾音', title in D('document.querySelector(".r0").textContent') and '拾音' in D('document.querySelector(".r1").textContent'), [D('document.querySelector("#rows").textContent'), title])
    dk.click('#tb [data-c=prev]'); time.sleep(1.0)
    check('toolbar 上一首 → back', S('curTrack().id') == t0)
    dk.click('#tb [data-a=bigger]'); time.sleep(0.4)
    check('字号 + saved (40)', desk_prefs().get('size') == 40, desk_prefs())
    dk.click('#tb [data-a=smaller]'); time.sleep(0.4)
    check('字号 − saved (36)', desk_prefs().get('size') == 36)
    dk.click('#tb [data-color=gold]'); time.sleep(0.4)
    check('颜色 preset saved + applied', desk_prefs().get('color') == 'gold' and D('document.body.classList.contains("c-gold")'))
    dk.click('#tb [data-color=rose]'); time.sleep(0.3)
    # text fits (no cut-off) with a very long line
    dk.evaluate('''() => { D.now = Object.assign({}, D.now, {lines: [{t: 0, text: "这是一句非常非常长的歌词用来测试桌面歌词窗口在字号很大的时候会不会被截断或者重叠在一起"}, {t: 999, text: "下一句"}], playing: false, pos: 1, offset: 0}); render(true); }''')
    fits = D('[...document.querySelectorAll(".row")].every(r => { const l = r.querySelector(".ln"); return !l || l.getBoundingClientRect().right <= r.getBoundingClientRect().right + 1 && l.getBoundingClientRect().left >= r.getBoundingClientRect().left - 1; })')
    check('long line shrinks to fit the window', fits)
    rows_ok = D('(() => { const a = document.querySelector(".r0").getBoundingClientRect(), b = document.querySelector(".r1").getBoundingClientRect(); return a.bottom <= b.top + 0.5 && b.bottom <= innerHeight; })()')
    check('two rows do not overlap and fit the height', rows_ok)
    # lock
    dk.click('#tb [data-a=lock]'); time.sleep(0.6)
    check('锁定 saved, toolbar hidden', desk_prefs().get('locked') is True and D('document.body.classList.contains("locked")') and D('getComputedStyle(document.querySelector("#tb")).opacity') == '0')
    check('window told locked', 'locked=true' in open('/tmp/app.log').read())
    check('main 词 shows locked', S('document.querySelector("#btnDesk").classList.contains("locked")'))
    # ---- 0.2.5 unlock pill + Ctrl+Alt+L ----
    pill_op = lambda: D('getComputedStyle(document.querySelector("#unlock")).opacity')
    check('locked, cursor away: unlock pill hidden', pill_op() == '0')
    check('deskLockHover(true) (window: cursor over lyrics) → pill shown', D('deskLockHover(true)') is True and (time.sleep(0.35) or pill_op()) == '1', pill_op())
    check('pill is lock icon only + hotkey in tooltip', D('document.querySelector("#unlock").textContent.trim()') == '' and D('!!document.querySelector("#unlock svg")') and 'Ctrl+Alt+L' in D('document.querySelector("#unlock").title'), D('document.querySelector("#unlock").title'))
    pr = D('(() => { const r = document.querySelector("#unlock").getBoundingClientRect(); return [r.left, r.top, r.right, r.bottom]; })()')
    check('pill = deskPillDef (top-centre, inside the toolbar strip, above the lyrics)', [round(v) for v in pr] == [425, 4, 455, 32] and pr[3] <= D('document.querySelector("#rows").getBoundingClientRect().top'), pr)
    check('toolbar stays hidden while locked', D('getComputedStyle(document.querySelector("#tb")).opacity') == '0')
    dk.screenshot(path='/tmp/d6.png', omit_background=True)
    D('deskLockHover(false)'); time.sleep(0.35)
    check('deskLockHover(false) → pill hidden again', pill_op() == '0')
    D('deskLockHover(true)'); time.sleep(0.35)
    dk.click('#unlock'); time.sleep(0.6)
    check('click pill → unlocked + toolbar back', desk_prefs().get('locked') is False and not D('document.body.classList.contains("locked")') and D('getComputedStyle(document.querySelector("#tb")).opacity') == '1')
    check('window told unlocked', 'locked=false' in open('/tmp/app.log').read().split('locked=true')[-1])
    check('pill gone after unlock', pill_op() == '0' and D('deskLockHover(true)') is False)
    dk.evaluate('fetch("/api/desklyrics/prefs", {method: "POST", body: JSON.stringify({toggleLock: true})})'); time.sleep(0.6)
    check('Ctrl+Alt+L (toggleLock) → locked', desk_prefs().get('locked') is True and D('document.body.classList.contains("locked")'))
    check('snapshot carries hotkey', D('D.hotkey') == 'Ctrl+Alt+L')
    pg.click('#btnDesk'); time.sleep(0.6)
    check('main 词 click unlocks (stays on)', desk_prefs().get('locked') is False and desk_prefs().get('on') is True)
    # settings rows
    pg.click('#btnSettings'); pg.wait_for_selector('#sDesk')
    check('设置 has 桌面歌词 switch (on)', S('document.querySelector("#sDesk").classList.contains("on")'))
    check('设置 → 歌词 shows 快捷键 Ctrl+Alt+L 锁定/解锁', S('(document.querySelector("#sDeskHotkey")||{}).textContent') == '快捷键 Ctrl+Alt+L 锁定/解锁')
    sec = pg.locator('.set-sec:has(h4:text-is("歌词"))')
    sec.scroll_into_view_if_needed(); time.sleep(0.3)
    bb = sec.bounding_box(); pg.screenshot(path=f'{PREV}/7_settings_hotkey.png', clip={'x': bb['x'] - 12, 'y': bb['y'] - 14, 'width': bb['width'] + 24, 'height': bb['height'] + 26})
    pg.keyboard.press('Escape'); time.sleep(0.3)
    # close from the desk toolbar
    dk.mouse.move(440, 80); time.sleep(0.2)
    dk.click('#tb [data-a=close]'); time.sleep(1.0)
    check('关闭 from desk → off in prefs + main button', desk_prefs().get('on') is False and not S('document.querySelector("#btnDesk").classList.contains("on")'))
    check('window told to hide', 'desk apply on=false' in open('/tmp/app.log').read())
    pg.click('#btnDesk'); time.sleep(0.5)
    check('re-open keeps size/color', desk_prefs().get('on') is True and desk_prefs().get('size') == 36 and desk_prefs().get('color') == 'rose')

    # ---------- previews ----------
    pg.evaluate('audio.pause()'); time.sleep(0.4)
    def demo(color, idx=1, prog=0.55, locked=False):
        dk.evaluate('([c, l]) => fetch("/api/desklyrics/prefs", {method: "POST", body: JSON.stringify({color: c, locked: l})})', [color, locked]); time.sleep(0.5)
        pos = DEMO[idx]['t'] + 4 * prog - 0.1
        dk.evaluate('([lines, pos]) => fetch("/api/desklyrics/now", {method: "POST", body: JSON.stringify({id: 999, title: "夏天的歌", artist: "拾音", playing: false, pos, at: Date.now(), offset: 0, lines})})', [DEMO, pos]); time.sleep(0.8)
    W, H = 1600, 900
    dw, dh = 880, height(36)
    dx, dy = (W - dw) // 2, H - dh - 70
    wp = wallpaper(W, H); wl = light_wallpaper(W, H)
    dk.mouse.move(-10, -10); dk.evaluate('document.body.classList.remove("hover")')
    # 6: locked, cursor over the lyrics → 「🔓 解锁」 pill (+ a drawn cursor on it)
    dk.evaluate('fetch("/api/desklyrics/prefs", {method: "POST", body: JSON.stringify({locked: true})})'); time.sleep(0.2)
    demo('rose', 1, 0.55, locked=True)
    dk.evaluate('deskLockHover(true)'); time.sleep(0.4)
    dk.screenshot(path='/tmp/d6.png', omit_background=True)
    composite('/tmp/d6.png', wp, dx, dy, f'{PREV}/6_locked_hover_unlock.png')
    im6 = Image.open(f'{PREV}/6_locked_hover_unlock.png'); cd = ImageDraw.Draw(im6); cx, cy = dx + 445, dy + 20
    cd.polygon([(cx, cy), (cx, cy + 19), (cx + 5, cy + 14), (cx + 9, cy + 22), (cx + 12, cy + 21), (cx + 8, cy + 13), (cx + 14, cy + 13)], fill='white', outline='black')
    im6.save(f'{PREV}/6_locked_hover_unlock.png')
    dk.evaluate('fetch("/api/desklyrics/prefs", {method: "POST", body: JSON.stringify({locked: false})})'); time.sleep(0.5)
    dk.evaluate('deskLockHover(false)')
    demo('rose', 1, 0.55)
    dk.screenshot(path='/tmp/d1.png', omit_background=True)
    composite('/tmp/d1.png', wp, dx, dy, f'{PREV}/1_desktop_unhovered.png')
    dk.mouse.move(440, 90); time.sleep(0.4)
    dk.screenshot(path='/tmp/d2.png', omit_background=True)
    composite('/tmp/d2.png', wp, dx, dy, f'{PREV}/2_hovered_toolbar.png')
    dk.mouse.move(-10, -10); dk.evaluate('document.body.classList.remove("hover")')
    demo('ink', 2, 0.4)
    dk.screenshot(path='/tmp/d4.png', omit_background=True)
    composite('/tmp/d4.png', wl, dx, dy, f'{PREV}/4_ink_on_light_wallpaper.png')
    demo('gold', 3, 0.7)
    dk.screenshot(path='/tmp/d5.png', omit_background=True)
    composite('/tmp/d5.png', wp, dx, dy, f'{PREV}/5_gold_preset.png')
    dk.evaluate('(c) => fetch("/api/desklyrics/prefs", {method: "POST", body: JSON.stringify({color: "rose"})})', 'rose')
    pg.evaluate('audio.play()'); time.sleep(0.8)
    pg.screenshot(path=f'{PREV}/3_player_bar_ci_active.png', clip={'x': 0, 'y': 800 - 120, 'width': 1280, 'height': 120})
    pg.screenshot(path=f'{PREV}/3b_main_window.png')
    check('no JS errors', not errors, errors)
    b.close()

fails = [r for r in results if not r[1]]
print(f'\n{len(results) - len(fails)}/{len(results)} passed')
json.dump([{'name': n, 'ok': o, 'info': str(i)} for n, o, i in results], open(os.path.join(T, 'e2e_v24_results.json'), 'w'), ensure_ascii=False, indent=1)
sys.exit(1 if fails else 0)
