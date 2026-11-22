package com.louis.myplayer

import android.content.Context
import android.net.wifi.WifiManager
import java.net.ConnectException
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.Inet4Address
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.NetworkInterface
import java.net.NoRouteToHostException
import java.net.Socket
import java.net.SocketTimeoutException
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/**
 * 2.2.1「网络诊断」(传到电脑 → 网络诊断): Wi-Fi SSID/BSSID/band, phone IP + gateway, VPN, reachability of
 * the gateway and the PC over the Wi-Fi interface, TCP to PC:18522 (exact error class), whether our own
 * server listens (127.0.0.1 and the Wi-Fi IP), and a UDP broadcast. Ends with a one-line verdict ([verdict]).
 */
object Diag {
    data class Facts(
        val wifi: Boolean = true,
        val pcGiven: Boolean = true,
        val sameSubnet: Boolean = true,
        val vpn: Boolean = false,
        val gateway: Int = -1,   // 1 reachable, 0 not, -1 not tried
        val pc: Int = -1,
        val tcp: String = "",    // ok | refused | timeout | unreachable | error
        val serverRunning: Boolean = true,
        val selfLocal: Boolean = true,
        val selfWifi: Boolean = true,
        val pcHotspot: Boolean = false, // phone is on the PC's Windows hotspot (192.168.137.x)
    )

    fun classify(e: Throwable?): String {
        if (e == null) return "ok"
        val m = (e.message ?: "").uppercase()
        return when {
            e is SocketTimeoutException || "ETIMEDOUT" in m || "TIMED OUT" in m -> "timeout"
            "ECONNREFUSED" in m || "REFUSED" in m -> "refused"
            e is NoRouteToHostException || "EHOSTUNREACH" in m || "ENETUNREACH" in m || "NO ROUTE" in m || "UNREACHABLE" in m -> "unreachable"
            e is ConnectException -> "error"
            else -> "error"
        }
    }

    private val tcpWords = mapOf("ok" to "连接成功", "refused" to "被拒绝（电脑没在监听，从手机导入窗口没开？）",
        "timeout" to "超时（数据包被丢弃）", "unreachable" to "主机不可达（ARP 失败，局域网里找不到电脑）", "error" to "出错")

    /** 2.3: recommended order when the router isolates Wi-Fi clients (PC hotspot last: it froze a laptop). */
    const val ISOLATION_ADVICE = "建议按顺序换一种方式：① 手机开热点，电脑连上手机的热点（只在两台设备之间传，不耗手机流量，保险起见可先关掉移动数据）；" +
        "② 用数据线把手机的 Music/拾音 文件夹复制到电脑；③ 登录路由器关闭 AP 隔离；④ 电脑开热点（不推荐，部分笔记本可能死机）"

    fun verdict(f: Facts): String {
        val v = when {
            !f.serverRunning -> "手机服务未运行 → 重新打开「传到电脑」页面"
            !f.selfLocal -> "手机服务没有在监听端口 → 关掉「传到电脑」页面再打开"
            !f.wifi -> "手机没连 Wi-Fi → 连上和电脑同一个 Wi-Fi，或者手机开热点、让电脑连上手机的热点"
            !f.pcGiven -> "没填电脑 IP，只检查了手机这边" + if (!f.selfWifi) "；手机自己的 Wi-Fi 地址连不上本机服务（系统拦截？）" else "：手机服务正常"
            !f.sameSubnet -> "电脑 IP 和手机不在同一个网段 → 填电脑「从手机导入」窗口里和手机同网段的那个 IP"
            f.tcp == "ok" -> "手机能连上电脑，网络是通的 → 用「连接电脑」即可"
            f.tcp == "refused" -> "能找到电脑但端口被拒绝 → 电脑拾音没打开「从手机导入」窗口"
            f.gateway == 1 && (f.tcp == "unreachable" || (f.pc == 0 && f.tcp == "timeout")) ->
                "能连网关但连不上电脑" + (if (f.tcp == "unreachable") "（ARP 失败）" else "") +
                    " → 路由器开了 AP 隔离（AP/客户端隔离；或手机连的是访客 Wi-Fi），同一个 Wi-Fi 下的设备互相访问不了。" + ISOLATION_ADVICE
            f.pc == 1 && f.tcp == "timeout" -> if (f.pcHotspot) "能 ping 通电脑但端口超时 → 电脑防火墙拦截（热点网络通常是“公用网络”，在防火墙提示里勾选“公用网络”允许拾音）"
                else "能 ping 通电脑但端口超时 → 电脑防火墙拦截（勾选“专用网络”允许拾音，或把网络改成专用网络）"
            f.gateway == 0 -> "手机连网关都不通 → 手机的 Wi-Fi 本身有问题"
            else -> "连不上电脑（${tcpWords[f.tcp] ?: f.tcp}）→ 很可能是路由器开了 AP 隔离。" + ISOLATION_ADVICE
        }
        return "结论：" + v + if (f.vpn && f.tcp != "ok") "（手机开着 VPN，已强制走 Wi-Fi 测试）" else ""
    }

    private fun tcp(host: String, port: Int, viaWifi: Boolean, timeout: Int = 3000): Pair<String, String> {
        val s = if (viaWifi) Net.socketFor(host) else Socket()
        val t0 = System.currentTimeMillis()
        return try {
            s.connect(InetSocketAddress(host, port), timeout)
            "ok" to "${System.currentTimeMillis() - t0} ms"
        } catch (e: Exception) {
            classify(e) to "${System.currentTimeMillis() - t0} ms：${e.javaClass.simpleName} ${e.message ?: ""}"
        } finally { try { s.close() } catch (_: Exception) {} }
    }

    private fun reach(ip: InetAddress, nif: NetworkInterface?): Boolean = try {
        if (nif != null) ip.isReachable(nif, 0, 1500) || ip.isReachable(nif, 0, 1500) else ip.isReachable(1500)
    } catch (_: Exception) { false }

    fun run(ctx: Context, pcHostRaw: String): String {
        val sb = StringBuilder()
        fun w(s: String) { sb.append(s).append('\n') }
        w("拾音 ${BuildConfig.VERSION_NAME} 网络诊断 · ${SyncServer.deviceName()} · ${SimpleDateFormat("yyyy-MM-dd HH:mm:ss", Locale.US).format(Date())}")
        var f = Facts()
        // Wi-Fi
        val lp = Net.wifiLink(ctx)
        val vpn = Net.vpnActive(ctx)
        f = f.copy(vpn = vpn, wifi = lp != null)
        try {
            @Suppress("DEPRECATION")
            val wi = (ctx.applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager).connectionInfo
            if (wi != null && lp != null) {
                val band = when (wi.frequency) { in 2400..2500 -> "2.4G"; in 4900..5900 -> "5G"; in 5925..7125 -> "6G"; else -> "?" }
                val ssid = wi.ssid?.trim('"')?.takeIf { it.isNotEmpty() && it != "<unknown ssid>" } ?: "（系统不给名称，需定位权限）"
                val bssid = wi.bssid?.takeIf { it != "02:00:00:00:00:00" } ?: "未知"
                w("【Wi-Fi】$ssid · BSSID $bssid · ${wi.frequency} MHz（$band）· 信号 ${wi.rssi} dBm")
            }
        } catch (_: Throwable) {}
        if (lp == null) w("【Wi-Fi】未连接")
        var gw: InetAddress? = null
        var wifiIp: Inet4Address? = null
        var nif: NetworkInterface? = null
        if (lp != null) {
            val la = lp.linkAddresses.firstOrNull { it.address is Inet4Address }
            wifiIp = la?.address as? Inet4Address
            gw = lp.routes.firstOrNull { it.isDefaultRoute && it.gateway is Inet4Address }?.gateway
            nif = try { NetworkInterface.getByName(lp.interfaceName) } catch (_: Exception) { null }
            w("【手机】${lp.interfaceName} ${la?.address?.hostAddress ?: "无 IPv4"}/${la?.prefixLength ?: "?"} · 网关 ${gw?.hostAddress ?: "无"}")
        }
        val others = SyncServer.candidates().filter { it.optString("kind") != "wifi" }
        if (others.isNotEmpty()) w("【其他网卡】" + others.joinToString("，") { "${it.optString("iface")} ${it.optString("ip")}/${it.optInt("prefix")} (${it.optString("kind")})" })
        w("【VPN】" + if (vpn) "开着（诊断时强制走 Wi-Fi）" else "没有")

        // own server
        val running = SyncServer.running
        f = f.copy(serverRunning = running)
        if (running) {
            val (l, ld) = tcp("127.0.0.1", SyncServer.port, false, 1500)
            f = f.copy(selfLocal = l == "ok")
            var line = "【手机服务】端口 ${SyncServer.port} 运行中；127.0.0.1 ${tcpWords[l]}（$ld）"
            if (wifiIp != null) {
                val (wr, wd) = tcp(wifiIp.hostAddress!!, SyncServer.port, false, 1500)
                f = f.copy(selfWifi = wr == "ok")
                line += "；${wifiIp.hostAddress} ${tcpWords[wr]}（$wd）"
            }
            w(line)
        } else w("【手机服务】未运行")

        // gateway
        if (gw != null) {
            val ok = reach(gw, nif)
            f = f.copy(gateway = if (ok) 1 else 0)
            w("【网关 ${gw.hostAddress}】" + if (ok) "能连通" else "不通")
        }

        // UDP broadcast
        try {
            DatagramSocket(null).use { s ->
                s.reuseAddress = true; s.broadcast = true; s.bind(InetSocketAddress(0))
                val bound = Net.bindToWifi(s)
                val msg = "SHIYIN_FIND_PC".toByteArray()
                s.send(DatagramPacket(msg, msg.size, InetAddress.getByName("255.255.255.255"), ReverseClient.PC_UDP_PORT))
                w("【UDP 广播】已发送" + if (bound) "（经 Wi-Fi）" else "")
            }
        } catch (e: Exception) { w("【UDP 广播】发送失败：${e.message}") }
        val found = try { ReverseClient.discover(1500) } catch (_: Exception) { emptyList() }
        w("【搜索电脑】" + if (found.isEmpty()) "没有电脑回应（电脑「从手机导入」窗口没开，或广播被隔离/防火墙拦截）" else found.joinToString("，") { "${it.optString("name")} ${it.optString("ip")}" })

        // PC
        val host = pcHostRaw.trim().replace('：', ':').substringBefore(':')
        val pcIp = try { if (host.isEmpty()) null else InetAddress.getByName(host) as? Inet4Address } catch (_: Exception) { null }
        if (pcIp == null) {
            f = f.copy(pcGiven = false)
            if (host.isNotEmpty()) w("电脑 IP「$host」格式不对")
        } else {
            val same = lp?.linkAddresses?.any { it.address is Inet4Address && Net.sameSubnet(it.address.address, pcIp.address, it.prefixLength) } == true ||
                SyncServer.candidates().any { k -> k.optString("kind") !in listOf("vpn", "mobile") && try { Net.sameSubnet(InetAddress.getByName(k.optString("ip")).address, pcIp.address, k.optInt("prefix", 24)) } catch (_: Exception) { false } }
            f = f.copy(sameSubnet = same, pcHotspot = host.startsWith("192.168.137."))
            if (!same) w("【电脑 $host】不在手机任何网络的网段里")
            val ok = reach(pcIp, if (same) nif else null)
            f = f.copy(pc = if (ok) 1 else 0)
            w("【ping 电脑 $host】" + if (ok) "能连通" else "不通")
            val (r, d) = tcp(host, ReverseClient.PC_PORT, true, 4000)
            f = f.copy(tcp = r)
            w("【TCP $host:${ReverseClient.PC_PORT}】${tcpWords[r]}（$d）")
        }
        w(verdict(f))
        return sb.toString()
    }
}
