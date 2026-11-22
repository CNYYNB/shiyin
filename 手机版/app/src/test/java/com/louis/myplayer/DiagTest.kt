package com.louis.myplayer

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.net.ConnectException
import java.net.NoRouteToHostException
import java.net.SocketTimeoutException

class DiagTest {
    private fun has(f: Diag.Facts, s: String) { val v = Diag.verdict(f); assertTrue("$v ⊅ $s", s in v) }

    @Test fun classify() {
        assertEquals("refused", Diag.classify(ConnectException("failed to connect to /192.168.0.103 (port 18522): connect failed: ECONNREFUSED (Connection refused)")))
        assertEquals("unreachable", Diag.classify(NoRouteToHostException("connect failed: EHOSTUNREACH (No route to host)")))
        assertEquals("timeout", Diag.classify(SocketTimeoutException("failed to connect after 4000ms")))
        assertEquals("ok", Diag.classify(null))
    }

    @Test fun verdicts() {
        // the user's real case: gateway fine, PC not resolvable (ARP) → AP isolation
        has(Diag.Facts(gateway = 1, pc = 0, tcp = "unreachable"), "AP/客户端隔离")
        has(Diag.Facts(gateway = 1, pc = 0, tcp = "unreachable"), "ARP 失败")
        has(Diag.Facts(gateway = 1, pc = 0, tcp = "timeout"), "AP/客户端隔离")
        has(Diag.Facts(serverRunning = false), "手机服务未运行")
        has(Diag.Facts(sameSubnet = false, tcp = "timeout"), "不在同一个网段")
        has(Diag.Facts(tcp = "ok"), "网络是通的")
        has(Diag.Facts(gateway = 1, pc = 1, tcp = "refused"), "从手机导入")
        has(Diag.Facts(gateway = 1, pc = 1, tcp = "timeout"), "防火墙")
        has(Diag.Facts(gateway = 1, pc = 1, tcp = "timeout", pcHotspot = true), "公用网络")
        has(Diag.Facts(wifi = false), "没连 Wi-Fi")
        has(Diag.Facts(vpn = true, gateway = 1, pc = 0, tcp = "unreachable"), "VPN")
    }

    @Test fun isolationAdvice() {
        val v = Diag.verdict(Diag.Facts(gateway = 1, pc = 0, tcp = "unreachable"))
        assertTrue(v, "路由器开了 AP 隔离" in v && "① 手机开热点" in v && "④ 电脑开热点（不推荐" in v)
        assertTrue(v.indexOf("① 手机开热点") < v.indexOf("② 用数据线") && v.indexOf("③ 登录路由器") < v.indexOf("④ 电脑开热点"))
    }

    @Test fun hotspotIface() {
        assertEquals("hotspot", SyncServer.ifaceKind("ap0", "wlan0", "192.168.43.1"))
        assertEquals("hotspot", SyncServer.ifaceKind("swlan0", null, "10.12.3.1"))
        assertEquals("hotspot", SyncServer.ifaceKind("wlan1", "wlan0", "192.168.43.1"))
        assertEquals("hotspot", SyncServer.ifaceKind("wlan2", null, "10.94.20.1"))
        assertEquals("wifi", SyncServer.ifaceKind("wlan0", "wlan0", "192.168.0.104"))
        assertEquals("wifi", SyncServer.ifaceKind("wlan0", null, "192.168.0.1"))
        assertEquals("mobile", SyncServer.ifaceKind("rmnet_data0", "wlan0", "10.20.30.40"))
        assertEquals("vpn", SyncServer.ifaceKind("tun0", "wlan0", "172.19.0.1"))
    }

    @Test fun subnet() {
        val a = byteArrayOf(192.toByte(), 168.toByte(), 0, 104.toByte())
        assertTrue(Net.sameSubnet(a, byteArrayOf(192.toByte(), 168.toByte(), 0, 103), 24))
        assertTrue(!Net.sameSubnet(a, byteArrayOf(192.toByte(), 168.toByte(), 137.toByte(), 1), 24))
        assertTrue(Net.sameSubnet(a, byteArrayOf(192.toByte(), 168.toByte(), 3, 1), 16))
    }

    @Test fun describeClient() {
        assertEquals("DESKTOP-1 (192.168.0.103)", SyncServer.describeClient("10.182.115.162", mapOf("x-shiyin-name" to "DESKTOP-1", "x-shiyin-from" to "192.168.0.103")))
        assertTrue("经 VPN" in SyncServer.describeClient("8.8.4.4", emptyMap()))
    }
}
