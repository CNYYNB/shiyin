package com.louis.myplayer

import android.content.Context
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import java.net.DatagramSocket
import java.net.Inet4Address
import java.net.InetAddress
import java.net.Socket

/**
 * 2.2.1: the phone's Wi-Fi [Network] (not the VPN), so LAN traffic to the PC (「手机连电脑」 TCP, UDP PC
 * discovery, diagnostics) can bypass a VPN app's routes. Every call is safe on the JVM (unit tests): it
 * returns null / does nothing when Android services aren't there.
 */
object Net {
    private fun cm(ctx: Context? = null): ConnectivityManager? = try {
        (ctx ?: Library.app).getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager
    } catch (_: Throwable) { null }

    fun wifiNetwork(ctx: Context? = null): Network? = try {
        val c = cm(ctx)
        c?.allNetworks?.firstOrNull { n ->
            val nc = c.getNetworkCapabilities(n)
            nc != null && nc.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) && !nc.hasTransport(NetworkCapabilities.TRANSPORT_VPN)
        }
    } catch (_: Throwable) { null }

    fun wifiLink(ctx: Context? = null): LinkProperties? = try { wifiNetwork(ctx)?.let { cm(ctx)?.getLinkProperties(it) } } catch (_: Throwable) { null }

    fun vpnActive(ctx: Context? = null): Boolean = try {
        val c = cm(ctx)
        c?.allNetworks?.any { c.getNetworkCapabilities(it)?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true } == true
    } catch (_: Throwable) { false }

    /** a.b.c.d on one of the Wi-Fi network's IPv4 subnets? */
    fun onWifiSubnet(ip: InetAddress, lp: LinkProperties? = wifiLink()): Boolean {
        if (ip !is Inet4Address || lp == null) return false
        return try { lp.linkAddresses.any { la -> la.address is Inet4Address && sameSubnet(la.address.address, ip.address, la.prefixLength) } } catch (_: Throwable) { false }
    }

    fun sameSubnet(a: ByteArray, b: ByteArray, prefix: Int): Boolean {
        if (a.size != 4 || b.size != 4) return false
        var bits = prefix.coerceIn(0, 32)
        for (i in 0 until 4) {
            val m = if (bits >= 8) 0xFF else if (bits <= 0) 0 else (0xFF shl (8 - bits)) and 0xFF
            if ((a[i].toInt() and m) != (b[i].toInt() and m)) return false
            bits -= 8
        }
        return true
    }

    /** An unconnected TCP socket that goes out via Wi-Fi when [host] is on the Wi-Fi subnet (VPN bypass). */
    fun socketFor(host: String): Socket {
        try {
            val ip = InetAddress.getByName(host)
            if (!ip.isLoopbackAddress) {
                val n = wifiNetwork()
                if (n != null && onWifiSubnet(ip)) return n.socketFactory.createSocket()
            }
        } catch (_: Throwable) {}
        return Socket()
    }

    /** Bind a datagram socket to the Wi-Fi network (VPN bypass). Returns true if bound. */
    fun bindToWifi(s: DatagramSocket): Boolean = try { val n = wifiNetwork(); if (n != null) { n.bindSocket(s); true } else false } catch (_: Throwable) { false }
}
