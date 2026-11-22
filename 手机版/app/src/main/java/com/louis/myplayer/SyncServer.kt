package com.louis.myplayer

import android.content.Context
import android.net.Uri
import android.net.wifi.WifiManager
import android.os.Build
import android.util.Log
import org.json.JSONArray
import org.json.JSONObject
import java.io.BufferedInputStream
import java.io.File
import java.io.InputStream
import java.io.OutputStream
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.Inet4Address
import java.net.InetSocketAddress
import java.net.NetworkInterface
import java.net.ServerSocket
import java.net.Socket
import java.net.SocketTimeoutException
import java.net.URLDecoder
import java.net.URLEncoder
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger

/**
 * 「传到电脑」：局域网 HTTP 服务 + UDP 发现。协议见 shiyin-sync-protocol.md。
 * 只读：电脑端只能读取歌曲、歌词、封面和歌单，不能修改或删除手机上的任何东西。
 */
object SyncServer {
    private const val TAG = "SyncServer"
    const val HTTP_PORT = 18520
    const val UDP_PORT = 18521

    @Volatile var running = false; private set
    @Volatile var port = 0; private set
    @Volatile var code = ""; private set
    val served = AtomicInteger(0)          // files sent this session
    @Volatile var lastClient = ""
    /** Last real request (not keep-alive), for the 30-minute idle stop in [SyncService]. */
    @Volatile var lastActivity = System.currentTimeMillis()
    fun touch() { lastActivity = System.currentTimeMillis() }

    private var server: ServerSocket? = null
    private var udp: DatagramSocket? = null
    private var pool = Executors.newFixedThreadPool(4)
    private var mlock: WifiManager.MulticastLock? = null
    private val app: Context get() = Library.app

    fun deviceName(): String = (Build.MODEL ?: "Android").trim()

    @Synchronized
    fun start(ctx: Context): Boolean {
        if (running) return true
        code = (100000 + java.security.SecureRandom().nextInt(900000)).toString()
        served.set(0); lastClient = ""; touch()
        var ss: ServerSocket? = null
        for (p in HTTP_PORT until HTTP_PORT + 10) {
            try { ss = ServerSocket().apply { reuseAddress = true; bind(InetSocketAddress(p)) }; port = p; break } catch (_: Exception) {}
        }
        if (ss == null) return false
        server = ss
        if (pool.isShutdown) pool = Executors.newFixedThreadPool(4)
        running = true
        Thread({ acceptLoop(ss) }, "sync-http").start()
        try {
            val wm = app.getSystemService(Context.WIFI_SERVICE) as WifiManager
            mlock = wm.createMulticastLock("shiyin-sync").apply { setReferenceCounted(false); acquire() }
        } catch (e: Exception) { Log.w(TAG, "mlock", e) }
        Thread({ udpLoop() }, "sync-udp").start()
        return true
    }

    @Synchronized
    fun stop() {
        running = false
        try { server?.close() } catch (_: Exception) {}
        try { udp?.close() } catch (_: Exception) {}
        server = null; udp = null
        try { mlock?.release() } catch (_: Exception) {}
        mlock = null
    }

    /**
     * Every IPv4 address of this phone the PC might reach, best first:
     * {ip, iface, kind: wifi|hotspot|eth|vpn|mobile|other}. Shown on the sheet so the user can try each one.
     */
    fun candidates(): List<JSONObject> {
        val out = mutableListOf<Pair<Int, JSONObject>>()
        // 2.3: the Wi-Fi client interface (usually wlan0); another wlanN with an address is the phone's own hotspot
        val sta = try { Net.wifiLink()?.interfaceName } catch (_: Throwable) { null }
        try {
            for (ni in NetworkInterface.getNetworkInterfaces()) {
                if (!ni.isUp || ni.isLoopback) continue
                for (ia in ni.interfaceAddresses) {
                    val a = ia.address
                    if (a !is Inet4Address || a.isLoopbackAddress || a.isLinkLocalAddress) continue
                    val kind = ifaceKind(ni.name, sta, a.hostAddress ?: "")
                    var rank = when (kind) { "wifi" -> 0; "hotspot" -> 1; "eth" -> 2; "other" -> 3; "vpn" -> 5; else -> 6 }
                    if (!a.isSiteLocalAddress) rank += 10
                    out += rank to JSONObject().put("ip", a.hostAddress).put("iface", ni.name).put("kind", kind).put("prefix", ia.networkPrefixLength.toInt())
                }
            }
        } catch (_: Exception) {}
        return out.sortedBy { it.first }.map { it.second }.distinctBy { it.optString("ip") }
    }

    /**
     * wifi | hotspot | eth | vpn | mobile | other for an interface (pure; unit-tested). [sta] = the Wi-Fi client
     * interface when known. Hotspot names vary by vendor: ap0, swlan0, softap0, or a second wlanN (wlan1/wlan2)
     * whose address is the subnet's .1 (192.168.43.1, 10.x.y.1…).
     */
    fun ifaceKind(name: String, sta: String?, ip: String): String {
        val n = name.lowercase()
        return when {
            n.startsWith("ap") || n.startsWith("swlan") || n.startsWith("softap") -> "hotspot"
            n.startsWith("wlan") && sta != null && !n.equals(sta, true) -> "hotspot"
            n.startsWith("wlan") && sta == null && n != "wlan0" && ip.endsWith(".1") -> "hotspot"
            n.startsWith("wlan") -> "wifi"
            n.startsWith("eth") || n.startsWith("rndis") || n.startsWith("usb") -> "eth"
            n.startsWith("tun") || n.startsWith("ppp") || n.startsWith("ipsec") || n.startsWith("wg") -> "vpn"
            n.startsWith("rmnet") || n.startsWith("ccmni") || n.startsWith("rev_rmnet") -> "mobile"
            else -> "other"
        }
    }

    /** Site-local IPv4 addresses, Wi-Fi first. */
    fun addresses(): List<String> {
        val out = mutableListOf<Pair<Int, String>>()
        try {
            for (ni in NetworkInterface.getNetworkInterfaces()) {
                if (!ni.isUp || ni.isLoopback) continue
                for (a in ni.inetAddresses) {
                    if (a is Inet4Address && a.isSiteLocalAddress) {
                        val n = ni.name.lowercase()
                        val rank = when { n.startsWith("wlan") -> 0; n.startsWith("ap") || n.startsWith("swlan") -> 1; n.startsWith("eth") -> 2; else -> 3 }
                        out += rank to a.hostAddress!!
                    }
                }
            }
        } catch (_: Exception) {}
        return out.sortedBy { it.first }.map { it.second }.distinct()
    }

    // ---------------- UDP discovery ----------------
    private fun udpLoop() {
        try {
            val s = DatagramSocket(null).apply { reuseAddress = true; broadcast = true; soTimeout = 1500; bind(InetSocketAddress(UDP_PORT)) }
            udp = s
            val buf = ByteArray(512)
            while (running) {
                val pk = DatagramPacket(buf, buf.size)
                try { s.receive(pk) } catch (_: SocketTimeoutException) { continue }
                val msg = String(pk.data, 0, pk.length, Charsets.UTF_8).trim()
                if (!msg.startsWith("SHIYIN_DISCOVER")) continue
                val ip = addresses().firstOrNull() ?: ""
                val reply = JSONObject().put("app", "shiyin").put("name", deviceName()).put("ip", ip)
                    .put("port", port).put("version", BuildConfig.VERSION_NAME).put("count", Library.tracks.size).toString().toByteArray(Charsets.UTF_8)
                try { s.send(DatagramPacket(reply, reply.size, pk.address, pk.port)) } catch (e: Exception) { Log.w(TAG, "udp reply", e) }
            }
        } catch (e: Exception) {
            if (running) Log.w(TAG, "udp", e)
        }
    }

    // ---------------- HTTP ----------------
    private fun acceptLoop(ss: ServerSocket) {
        while (running) {
            val sock = try { ss.accept() } catch (_: Exception) { break }
            pool.execute { try { handle(sock) } catch (e: Exception) { Log.w(TAG, "handle", e) } finally { try { sock.close() } catch (_: Exception) {} } }
        }
    }

    private class Req(val method: String, val path: String, val query: Map<String, String>, val headers: Map<String, String>)

    private fun readLine(i: InputStream): String? {
        val sb = StringBuilder()
        while (true) {
            val c = i.read()
            if (c < 0) return if (sb.isEmpty()) null else sb.toString()
            if (c == '\n'.code) return sb.toString().trimEnd('\r')
            sb.append(c.toChar())
            if (sb.length > 8192) return null
        }
    }

    private fun parse(i: InputStream, sock: Socket? = null): Req? {
        var first = readLine(i) ?: return null
        if (sock != null) { // reverse connection: the PC sends blank keep-alive lines while the connection waits in its pool
            while (first.isEmpty()) first = readLine(i) ?: return null
            sock.soTimeout = 30000
        }
        val parts = first.split(' ')
        if (parts.size < 2) return null
        val headers = HashMap<String, String>()
        while (true) {
            val l = readLine(i) ?: break
            if (l.isEmpty()) break
            val k = l.substringBefore(':').trim().lowercase()
            headers[k] = l.substringAfter(':', "").trim()
        }
        val target = parts[1]
        val path = target.substringBefore('?')
        val q = HashMap<String, String>()
        if (target.contains('?')) for (kv in target.substringAfter('?').split('&')) {
            if (kv.isEmpty()) continue
            val k = URLDecoder.decode(kv.substringBefore('='), "UTF-8")
            val v = URLDecoder.decode(kv.substringAfter('=', ""), "UTF-8")
            q[k] = v
        }
        return Req(parts[0].uppercase(), path, q, headers)
    }

    private fun head(o: OutputStream, status: Int, type: String, len: Long, extra: Map<String, String> = emptyMap()) {
        val reason = when (status) { 200 -> "OK"; 206 -> "Partial Content"; 204 -> "No Content"; 401 -> "Unauthorized"; 404 -> "Not Found"; 416 -> "Range Not Satisfiable"; else -> "Error" }
        val sb = StringBuilder("HTTP/1.1 $status $reason\r\n")
        sb.append("Content-Type: $type\r\n")
        if (len >= 0) sb.append("Content-Length: $len\r\n")
        sb.append("Access-Control-Allow-Origin: *\r\nAccess-Control-Allow-Headers: X-Shiyin-Code, Range\r\nAccess-Control-Expose-Headers: Content-Length, Content-Range, Content-Disposition\r\n")
        sb.append("Connection: close\r\nCache-Control: no-store\r\n")
        for ((k, v) in extra) sb.append("$k: $v\r\n")
        sb.append("\r\n")
        o.write(sb.toString().toByteArray(Charsets.UTF_8))
    }

    private fun text(o: OutputStream, status: Int, body: String, type: String = "application/json; charset=utf-8", isHead: Boolean = false) {
        val b = body.toByteArray(Charsets.UTF_8)
        head(o, status, type, b.size.toLong())
        if (!isHead) o.write(b)
        o.flush()
    }

    /**
     * Serves one HTTP request that arrives on an outbound connection the phone opened to the PC
     * (「手机连电脑」, see [ReverseClient]). The PC was authenticated with its pairing code during the
     * handshake, so no code is needed here. Blocks until the PC sends a request (or [idleMs] without
     * keep-alive). Returns true if a request was served.
     */
    fun serveReverse(sock: Socket, i: InputStream, pcName: String, idleMs: Int): Boolean {
        sock.soTimeout = idleMs
        return handle(sock, i, reversePc = pcName)
    }

    private fun handle(sock: Socket) { handle(sock, BufferedInputStream(sock.getInputStream()), null) }

    private fun handle(sock: Socket, i: InputStream, reversePc: String?): Boolean {
        if (reversePc == null) sock.soTimeout = 30000
        val o = sock.getOutputStream().buffered(1 shl 16)
        val r = parse(i, if (reversePc != null) sock else null) ?: return false
        val isHead = r.method == "HEAD"
        if (r.method == "OPTIONS") { head(o, 204, "text/plain", 0); o.flush(); return true }
        if (r.path == "/api/ping" || r.path == "/") {
            text(o, 200, JSONObject().put("app", "shiyin").put("version", BuildConfig.VERSION_NAME).put("name", deviceName()).put("needCode", reversePc == null).toString(), isHead = isHead)
            return true
        }
        if (reversePc == null) {
            val given = r.query["code"] ?: r.headers["x-shiyin-code"] ?: ""
            if (given != code) { text(o, 401, "{\"error\":\"配对码不对\"}", isHead = isHead); return true }
            // 2.2.1: only an authenticated /api/library counts as 「已连接电脑」 (see below), not a probe
        } else lastClient = reversePc
        touch()
        when (r.path) {
            "/api/library" -> {
                text(o, 200, library().toString(), isHead = isHead)
                if (reversePc == null) lastClient = describeClient(sock.inetAddress?.hostAddress ?: "", r.headers)
            }
            "/api/file" -> {
                val t = find(r.query["key"]) ?: run { text(o, 404, "{\"error\":\"没有这首歌\"}", isHead = isHead); return true }
                sendFile(o, t, r.headers["range"], isHead)
            }
            "/api/lrc" -> {
                val t = find(r.query["key"]) ?: run { text(o, 404, "{}", isHead = isHead); return true }
                val l = try { Lyrics.saved(app, t) } catch (_: Exception) { null }
                if (l.isNullOrBlank()) text(o, 404, "{}", isHead = isHead) else text(o, 200, l, "text/plain; charset=utf-8", isHead)
            }
            "/api/cover" -> {
                val t = find(r.query["key"]) ?: run { text(o, 404, "{}", isHead = isHead); return true }
                val f = try { Covers.file(app, t) } catch (_: Exception) { null }
                if (f == null || !f.exists()) { text(o, 404, "{}", isHead = isHead); return true }
                head(o, 200, "image/jpeg", f.length())
                if (!isHead) f.inputStream().use { it.copyTo(o) }
                o.flush()
            }
            else -> text(o, 404, "{}", isHead = isHead)
        }
        touch()
        return true
    }

    /**
     * 「电脑名 (局域网 IP)」 for the status line. PC 0.2.2 sends X-Shiyin-Name / X-Shiyin-From (its LAN address
     * on the phone's subnet); when the connection arrives from a non-LAN address (VPN / NAT) say so.
     */
    fun describeClient(remote: String, headers: Map<String, String>): String {
        val name = try { URLDecoder.decode(headers["x-shiyin-name"] ?: "", "UTF-8").trim() } catch (_: Exception) { "" }.take(40)
        val from = (headers["x-shiyin-from"] ?: "").trim().takeIf { Regex("""^\d{1,3}(\.\d{1,3}){3}$""").matches(it) }
        val lan = from ?: remote
        val isLan = from != null || try {
            val a = java.net.InetAddress.getByName(lan)
            val subs = candidates().filter { it.optString("kind") != "vpn" && it.optString("kind") != "mobile" }
            if (a.isLoopbackAddress) true
            else if (subs.isEmpty()) a.isSiteLocalAddress
            else subs.any { Net.sameSubnet(java.net.InetAddress.getByName(it.optString("ip")).address, a.address, it.optInt("prefix", 24)) }
        } catch (_: Exception) { false }
        val ip = if (isLan) lan else "$lan，经 VPN/其他网络"
        return if (name.isNotEmpty()) "$name ($ip)" else ip
    }

    private fun find(key: String?): Track? {
        if (key.isNullOrEmpty()) return null
        return Library.tracks.firstOrNull { it.key == key }
    }

    private fun ext(t: Track) = (t.fileName.ifEmpty { t.path.substringAfterLast('/') }).substringAfterLast('.', "").lowercase()

    private fun library(): JSONObject {
        val arr = JSONArray()
        val seen = HashSet<String>()
        for (raw in Library.tracks) {
            if (!seen.add(raw.key)) continue
            val d = Library.display(raw)
            val hasCover = Covers.customFile(app, raw).exists() || try { Covers.file(app, raw) != null } catch (_: Exception) { false }
            val lrc = try { Lyrics.saved(app, raw) } catch (_: Exception) { null }
            arr.put(JSONObject()
                .put("key", raw.key).put("title", d.title).put("artist", d.artist).put("album", d.album)
                .put("rawTitle", raw.title).put("rawArtist", raw.artist).put("rawAlbum", raw.album)
                .put("duration", raw.duration).put("size", raw.size)
                .put("fileName", raw.fileName.ifEmpty { raw.path.substringAfterLast('/') }).put("ext", ext(raw))
                .put("trackNo", raw.trackNo).put("dateAdded", raw.dateAdded)
                .put("hasLrc", !lrc.isNullOrBlank()).put("lrcSynced", lrc != null && Lyrics.isSynced(lrc))
                .put("hasCover", hasCover).put("coverVer", Covers.customVersion(app, raw)))
        }
        val pls = JSONArray()
        try {
            val p = JSONArray(Kv.get(app, "playlists") ?: "[]")
            for (n in 0 until p.length()) {
                val pl = p.getJSONObject(n)
                val ids = pl.optJSONArray("ids") ?: JSONArray()
                val keys = JSONArray()
                for (j in 0 until ids.length()) Library.get(ids.optString(j))?.let { keys.put(it.key) }
                pls.put(JSONObject().put("name", pl.optString("name")).put("fav", pl.optString("id") == "fav").put("keys", keys))
            }
        } catch (_: Exception) {}
        return JSONObject().put("app", "shiyin").put("version", BuildConfig.VERSION_NAME).put("name", deviceName())
            .put("tracks", arr).put("playlists", pls)
    }

    private fun mime(e: String) = when (e) {
        "mp3" -> "audio/mpeg"; "flac" -> "audio/flac"; "m4a", "mp4", "aac" -> "audio/mp4"; "ogg", "oga" -> "audio/ogg"
        "opus" -> "audio/opus"; "wav" -> "audio/wav"; "webm" -> "audio/webm"; else -> "application/octet-stream"
    }

    private fun open(t: Track): InputStream? {
        if (t.path.isNotEmpty()) { val f = File(t.path); if (f.canRead()) return f.inputStream() }
        return try { app.contentResolver.openInputStream(Uri.parse(t.uri)) } catch (_: Exception) { null }
    }

    private fun sendFile(o: OutputStream, t: Track, range: String?, isHead: Boolean) {
        val total = if (t.path.isNotEmpty() && File(t.path).exists()) File(t.path).length() else t.size
        val input = open(t) ?: return text(o, 404, "{\"error\":\"文件读不到\"}", isHead = isHead)
        input.use { s ->
            var start = 0L; var end = total - 1; var status = 200
            if (range != null && range.startsWith("bytes=") && total > 0) {
                val spec = range.removePrefix("bytes=").substringBefore(',')
                val a = spec.substringBefore('-').trim(); val b = spec.substringAfter('-', "").trim()
                if (a.isEmpty() && b.isNotEmpty()) { start = (total - b.toLong()).coerceAtLeast(0) }
                else { start = a.toLongOrNull() ?: 0; if (b.isNotEmpty()) end = minOf(b.toLong(), total - 1) }
                if (start > end || start >= total) { head(o, 416, "text/plain", 0, mapOf("Content-Range" to "bytes */$total")); o.flush(); return }
                status = 206
            }
            val len = end - start + 1
            val fname = t.fileName.ifEmpty { t.path.substringAfterLast('/') }.ifEmpty { "song" }
            val extra = mutableMapOf(
                "Accept-Ranges" to "bytes",
                "Content-Disposition" to "attachment; filename*=UTF-8''" + URLEncoder.encode(fname, "UTF-8").replace("+", "%20"),
            )
            if (status == 206) extra["Content-Range"] = "bytes $start-$end/$total"
            head(o, status, mime(ext(t)), len, extra)
            if (isHead) { o.flush(); return }
            var skip = start
            while (skip > 0) { val n = s.skip(skip); if (n <= 0) break; skip -= n }
            val buf = ByteArray(1 shl 16)
            var left = len
            while (left > 0) {
                val n = s.read(buf, 0, minOf(buf.size.toLong(), left).toInt())
                if (n < 0) break
                o.write(buf, 0, n); left -= n
            }
            o.flush()
            if (start == 0L && left == 0L) served.incrementAndGet()
        }
    }
}
