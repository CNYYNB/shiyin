package com.louis.myplayer

import android.util.Log
import org.json.JSONObject
import java.io.BufferedInputStream
import java.io.InputStream
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.Inet4Address
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.NetworkInterface
import java.net.Socket
import java.net.SocketTimeoutException
import java.util.Collections

/**
 * 「手机连电脑」(2.2)：电脑连不进手机时（路由器 AP 隔离、手机 VPN、澎湃 OS 拦截入站连接…），改由手机主动连电脑。
 *
 * 手机向电脑 TCP 18522 打开 [SLOTS] 条连接，每条先握手（配对码 = 电脑上显示的 6 位数），然后在这条连接上
 * 等待电脑发来一个普通的 HTTP 请求（/api/library、/api/file…），由 [SyncServer.serveReverse] 按原来的协议回复，
 * 回复完关闭，再开一条新的补上。所以电脑端的导入功能完全不用改，只是“拨号”变成了从这个连接池里取连接。
 * 协议见 shiyin-sync-protocol.md「反向连接」。
 */
object ReverseClient {
    private const val TAG = "ReverseClient"
    const val PC_PORT = 18522
    const val PC_UDP_PORT = 18523
    private const val SLOTS = 4
    private const val IDLE_MS = 45_000 // the PC sends a keep-alive line every 10 s

    @Volatile var state = "idle"; private set     // idle | connecting | connected | retrying | error
    @Volatile var message = ""; private set
    @Volatile var pcName = ""; private set
    @Volatile var pcHost = ""; private set
    @Volatile private var gen = 0
    @Volatile private var lastOk = 0L
    private val live = Collections.synchronizedSet(HashSet<Socket>())

    fun statusJson(): JSONObject = JSONObject().put("state", state).put("msg", message).put("pc", pcName).put("host", pcHost)
        .put("conns", live.size)

    fun statusLine(): String = when (state) {
        "connected" -> "已连上电脑 $pcName · 已传 ${SyncServer.served.get()} 首"
        "retrying" -> "和电脑 $pcName 的连接断了，正在重连…"
        "connecting" -> "正在连接电脑…"
        else -> ""
    }

    // ---------------- discovery ----------------
    /** Broadcasts SHIYIN_FIND_PC on UDP 18523; PCs with 「从手机导入」 open answer with JSON. */
    fun discover(waitMs: Long = 2500): List<JSONObject> {
        // 2.2.1: one socket on the default route (hotspot/other LANs) + one bound to the Wi-Fi network so
        // a phone VPN can't swallow the broadcast; replies are collected from both.
        val out = Collections.synchronizedMap(LinkedHashMap<String, JSONObject>())
        val socks = mutableListOf<DatagramSocket>()
        try {
            fun mk(): DatagramSocket = DatagramSocket(null).apply { reuseAddress = true; broadcast = true; bind(InetSocketAddress(0)) }
            socks += mk()
            try { val w = mk(); if (Net.bindToWifi(w)) socks += w else w.close() } catch (_: Exception) {}
            val msg = "SHIYIN_FIND_PC".toByteArray()
            val targets = broadcastTargets()
            fun send() { for (s in socks) for (t in targets) try { s.send(DatagramPacket(msg, msg.size, t, PC_UDP_PORT)) } catch (_: Exception) {} }
            val deadline = System.currentTimeMillis() + waitMs
            val threads = socks.map { s -> Thread {
                val buf = ByteArray(2048)
                while (true) {
                    val left = deadline - System.currentTimeMillis()
                    if (left <= 0) break
                    s.soTimeout = left.toInt().coerceAtLeast(50)
                    val pk = DatagramPacket(buf, buf.size)
                    try { s.receive(pk) } catch (_: SocketTimeoutException) { break } catch (_: Exception) { break }
                    try {
                        val o = JSONObject(String(pk.data, 0, pk.length, Charsets.UTF_8))
                        if (o.optString("app") != "shiyin-pc") continue
                        val ip = pk.address.hostAddress ?: continue
                        o.put("ip", ip)
                        if (o.optInt("port") == 0) o.put("port", PC_PORT)
                        out["$ip:${o.optInt("port")}"] = o
                    } catch (_: Exception) {}
                }
            }.apply { isDaemon = true; start() } }
            send()
            try { Thread.sleep(waitMs / 2) } catch (_: InterruptedException) {}
            send()
            threads.forEach { it.join(waitMs + 500) }
        } catch (e: Exception) { Log.w(TAG, "discover", e) }
        finally { socks.forEach { try { it.close() } catch (_: Exception) {} } }
        return synchronized(out) { out.values.toList() }
    }

    private fun broadcastTargets(): List<InetAddress> {
        val out = mutableListOf<InetAddress>(InetAddress.getByName("255.255.255.255"))
        try {
            for (ni in NetworkInterface.getNetworkInterfaces()) {
                if (!ni.isUp || ni.isLoopback) continue
                for (ia in ni.interfaceAddresses) {
                    if (ia.address is Inet4Address) ia.broadcast?.let { if (it !in out) out += it }
                }
            }
        } catch (_: Exception) {}
        return out
    }

    // ---------------- connection ----------------
    private class Hello(val sock: Socket, val input: InputStream, val pc: JSONObject)
    private class HandshakeError(msg: String, val fatal: Boolean) : Exception(msg)

    private fun parseHost(host: String): Pair<String, Int> {
        var h = host.trim().replace('：', ':').removePrefix("http://").trimEnd('/')
        var port = PC_PORT
        if (h.count { it == ':' } == 1) {
            port = h.substringAfter(':').toIntOrNull() ?: throw HandshakeError("端口不对", true)
            h = h.substringBefore(':')
        }
        if (h.isEmpty()) throw HandshakeError("请输入电脑上显示的 IP 地址", true)
        return h to port
    }

    private fun readLineRaw(i: InputStream): String? {
        val b = java.io.ByteArrayOutputStream()
        while (true) {
            val c = i.read()
            if (c < 0) return if (b.size() == 0) null else b.toString("UTF-8")
            if (c == '\n'.code) return b.toString("UTF-8").trimEnd('\r')
            b.write(c)
            if (b.size() > 8192) return null
        }
    }

    private fun handshake(host: String, port: Int, code: String): Hello {
        val s = Net.socketFor(host) // 2.2.1: via the Wi-Fi network when the PC is on it (bypasses a phone VPN)
        try {
            s.tcpNoDelay = true
            try { s.connect(InetSocketAddress(host, port), 5000) }
            catch (e: Exception) { throw HandshakeError(connectError(host, port, e), false) }
            s.soTimeout = 8000
            val hello = JSONObject().put("app", "shiyin").put("name", SyncServer.deviceName())
                .put("version", BuildConfig.VERSION_NAME).put("count", Library.tracks.size)
            s.getOutputStream().apply { write("SHIYIN-REVERSE/1 $code $hello\n".toByteArray(Charsets.UTF_8)); flush() }
            // read byte by byte so nothing after the reply line is swallowed before SyncServer reads the request
            val raw = s.getInputStream()
            val line = try { readLineRaw(raw) } catch (_: SocketTimeoutException) { null }
                ?: throw HandshakeError("电脑没有回应（$host:$port 不是拾音电脑版？）", false)
            if (line.startsWith("OK")) {
                val pc = try { JSONObject(line.substringAfter(' ', "{}")) } catch (_: Exception) { JSONObject() }
                return Hello(s, BufferedInputStream(raw), pc)
            }
            if (line.startsWith("ERR")) {
                val why = line.split(' ', limit = 3)
                val msg = when (why.getOrNull(1)) {
                    "badcode" -> "配对码不对，请输入电脑上显示的 6 位配对码"
                    "busy" -> "电脑正在连接另一台手机"
                    else -> why.getOrNull(2) ?: "电脑拒绝了连接"
                }
                throw HandshakeError(msg, true)
            }
            throw HandshakeError("这个地址不是拾音电脑版", true)
        } catch (e: Exception) {
            try { s.close() } catch (_: Exception) {}
            throw e
        }
    }

    private fun connectError(host: String, port: Int, e: Exception): String = when (Diag.classify(e)) {
        "refused" -> "电脑 $host 拒绝连接：电脑拾音没有打开「从手机导入」窗口（或端口不是 $port）"
        "unreachable" -> "找不到电脑 $host（网络不可达）。同一个 Wi-Fi 还连不上多半是路由器开了“AP 隔离”，可以让手机连电脑的移动热点"
        "timeout" -> "连接电脑 $host:$port 超时：电脑防火墙拦截（勾选“专用网络/公用网络”允许拾音），或路由器开了“AP 隔离”"
        else -> "连不上电脑 $host:$port（${e.message ?: e.javaClass.simpleName}）。请确认电脑拾音已打开「从手机导入」"
    }

    /** Blocking: first handshake, then the connection pool runs in background threads. Returns {ok, msg, pc}. */
    fun connect(host: String, code: String): JSONObject {
        stop()
        val c = code.filter { it.isDigit() }
        if (c.length != 6) return JSONObject().put("ok", false).put("msg", "配对码是电脑上显示的 6 位数字")
        val my: Int
        synchronized(this) { gen++; my = gen }
        state = "connecting"; message = ""; pcHost = host.trim()
        val hp = try { parseHost(host) } catch (e: HandshakeError) { state = "error"; message = e.message ?: ""; return JSONObject().put("ok", false).put("msg", message) }
        val first = try { handshake(hp.first, hp.second, c) } catch (e: Exception) {
            if (gen == my) { state = "error"; message = e.message ?: "连接失败" }
            return JSONObject().put("ok", false).put("msg", e.message ?: "连接失败")
        }
        if (gen != my) { try { first.sock.close() } catch (_: Exception) {}; return JSONObject().put("ok", false).put("msg", "已取消") }
        pcName = first.pc.optString("name").ifEmpty { hp.first }
        pcHost = "${hp.first}:${hp.second}"
        state = "connected"; lastOk = System.currentTimeMillis()
        SyncServer.lastClient = pcName
        SyncServer.touch()
        for (n in 0 until SLOTS) {
            val seed = if (n == 0) first else null
            Thread({ slotLoop(my, hp.first, hp.second, c, seed) }, "reverse-$n").apply { isDaemon = true; start() }
        }
        return JSONObject().put("ok", true).put("pc", pcName).put("host", pcHost)
    }

    private fun slotLoop(my: Int, host: String, port: Int, code: String, seed: Hello?) {
        var next = seed
        var fails = 0
        while (gen == my) {
            val h = next ?: try { handshake(host, port, code) } catch (e: HandshakeError) {
                if (e.fatal) {
                    if (gen == my) { state = "error"; message = e.message ?: ""; stop() }
                    return
                }
                null
            } catch (_: Exception) { null }
            next = null
            if (h == null) {
                fails++
                if (System.currentTimeMillis() - lastOk > 12_000 && gen == my) state = "retrying"
                try { Thread.sleep(minOf(5000L, 700L * fails)) } catch (_: InterruptedException) { return }
                continue
            }
            fails = 0; lastOk = System.currentTimeMillis()
            if (gen == my && state != "connected") { state = "connected"; message = "" }
            live += h.sock
            try {
                SyncServer.serveReverse(h.sock, h.input, pcName, IDLE_MS)
            } catch (e: Exception) {
                Log.d(TAG, "slot: ${e.message}")
            } finally {
                live -= h.sock
                try { h.sock.close() } catch (_: Exception) {}
            }
        }
    }

    fun stop() {
        synchronized(this) { gen++ }
        val socks = synchronized(live) { live.toList() }
        for (s in socks) try { s.close() } catch (_: Exception) {}
        live.clear()
        if (state != "error") { state = "idle"; message = "" }
    }

    fun reset() { stop(); state = "idle"; message = "" }
}
